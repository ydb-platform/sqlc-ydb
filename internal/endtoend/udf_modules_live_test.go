package endtoend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestLiveYDBUDFModules(t *testing.T) {
	cases := []struct {
		name, sql string
		values    map[string]any
	}{
		{"text", `SELECT String::Base32Encode("book") AS encoded, String::Reverse("book") AS reversed,
            String::HasPrefix("book", "bo") AS prefix, Unicode::IsAlpha("Book"u) AS alphabetic,
            Unicode::Reverse("Book"u) AS unicode_reversed, Url::GetHost("https://example.org/books") AS host,
            Url::GetScheme("https://example.org/books") AS scheme;`,
			map[string]any{"encoded": "MJXW62Y=", "reversed": "koob", "prefix": true, "alphabetic": true, "unicode_reversed": "kooB", "host": "example.org", "scheme": "https://"}},
		{"text_collections", `SELECT Yson::SerializeJson(Yson::From(String::SplitToList("a,b", ","))) AS words,
            Yson::SerializeJson(Yson::From(Unicode::SplitToList("a,b"u, ","u))) AS unicode_words,
            Yson::SerializeJson(Yson::From(Url::QueryStringToList("a=1&b=2"))) AS query_pairs;`,
			map[string]any{"words": []any{"a", "b"}, "unicode_words": []any{"a", "b"}, "query_pairs": []any{[]any{"a", "1"}, []any{"b", "2"}}}},
		{"numeric", `SELECT Math::Sqrt(9.0) AS root, Math::Pow(2.0, 3.0) AS power,
            Math::Mod(10l, 3l) AS remainder, Digest::Md5Hex("abc") AS md5;`,
			map[string]any{"root": 3.0, "power": 8.0, "remainder": 1, "md5": "900150983cd24fb0d6963f7d28e17f72"}},
		{"network", `SELECT Ip::IsIPv4(Ip::FromString("127.0.0.1")) AS ipv4,
            Ip::ToString(Ip::FromString("127.0.0.1")) AS address;`,
			map[string]any{"ipv4": true, "address": "127.0.0.1"}},
		{"datetime", `SELECT DateTime::GetYear(DateTime::Split(DateTime::FromSeconds(0u))) AS year,
            DateTime::ToSeconds(DateTime::FromSeconds(42u)) AS seconds,
            DateTime::ToDays(DateTime::IntervalFromDays(2)) AS days;`,
			map[string]any{"year": 1970, "seconds": 42, "days": 2}},
		{"documents", `SELECT Yson::IsString(Yson::From("book")) AS is_string,
		    Yson::LookupString(Yson::ParseJson(CAST("{\"name\":\"book\"}" AS Json)), "name") AS name,
		    Yson::SerializeJson(Json::From(String::SplitToList("a,b", ","))) AS numbers,
		    Yson::ConvertTo(Yson::From(42u), Uint32) AS converted,
		    Yson::GetLength(Yson::From(String::SplitToList("a,b", ","))) AS length;`,
			map[string]any{"is_string": true, "name": "book", "numbers": []any{"a", "b"}, "converted": 42, "length": 2}},
		{"document_options", `SELECT Yson::SerializeJson(Yson::ParseJson("{", Yson::Options(false AS Strict))) AS malformed,
		    Yson::SerializeJson(Json::From(AsList())) AS empty_list,
		    Yson::SerializeJson(Yson::ParseJson(NULL)) AS null_parse;`,
			map[string]any{"malformed": nil, "empty_list": []any{}, "null_parse": nil}},
		{"document_struct_lambda", `$parse = ($raw) -> {
            $stored = Yson::ConvertTo(Yson::ParseJson($raw), Struct<status:String?>);
            RETURN <| status: COALESCE($stored.status, "unknown") |>;
        };
        $get = ($raw) -> (($parse($raw)).status);
        $maybe = Yson::ConvertTo(Yson::ParseJson("{\"status\":\"ready\"}"), Struct<status:String?>);
        SELECT $get(doc) AS resolved, $maybe.status AS optional_status
        FROM (SELECT "{\"status\":\"ready\"}" AS doc) AS source;`,
			map[string]any{"resolved": "ready", "optional_status": "ready"}},
		{"regex", `SELECT Pire::Grep("bo")("book") AS pire,
            Re2::Grep("bo")("book") AS re2,
            Re2::Capture("(?P<word>bo+)")("book").word AS capture;`,
			map[string]any{"pire": true, "re2": true, "capture": "boo"}},
		{"bitmaps", `SELECT Roaring::Cardinality(Roaring::FromUint32List(COALESCE(Yson::ConvertTo(Yson::ParseJson(CAST("[1,2,2]" AS Json)), List<Uint32>), ListCreate(Uint32)))) AS cardinality,
		    Roaring::Cardinality(Roaring::And(Roaring::FromUint32List(COALESCE(Yson::ConvertTo(Yson::ParseJson(CAST("[1,2]" AS Json)), List<Uint32>), ListCreate(Uint32))), Roaring::FromUint32List(COALESCE(Yson::ConvertTo(Yson::ParseJson(CAST("[2,3]" AS Json)), List<Uint32>), ListCreate(Uint32))))) AS intersection;`,
			map[string]any{"cardinality": 2, "intersection": 1}},
		{"vectors", `SELECT Knn::InnerProductSimilarity(Knn::ToBinaryStringFloat(Yson::ConvertTo(Yson::ParseJson(CAST("[1.0,2.0]" AS Json)), List<Float>)), Knn::ToBinaryStringFloat(Yson::ConvertTo(Yson::ParseJson(CAST("[2.0,3.0]" AS Json)), List<Float>))) AS dot,
		    Knn::CosineSimilarity(Knn::ToBinaryStringFloat(Yson::ConvertTo(Yson::ParseJson(CAST("[1.0,0.0]" AS Json)), List<Float>)), Knn::ToBinaryStringFloat(Yson::ConvertTo(Yson::ParseJson(CAST("[1.0,0.0]" AS Json)), List<Float>))) AS cosine;`,
			map[string]any{"dot": 8.0, "cosine": 1.0}},
	}
	type column struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type liveCase struct {
		Name      string         `json:"name"`
		SQL       string         `json:"sql"`
		Columns   []column       `json:"columns"`
		Values    map[string]any `json:"values"`
		ErrorText string         `json:"error_text,omitempty"`
	}
	live := make([]liveCase, 0, len(cases))
	for _, tc := range cases {
		analysis, err := analyzer.Analyze(nil, []model.Source{{Name: tc.name + ".sql", Text: "-- name: Check :one\n" + tc.sql}})
		if err != nil {
			assert.Fail(t, fmt.Sprintf("%s: %v", tc.name, err))
			continue
		}
		query := analysis.Queries[0]
		entry := liveCase{Name: tc.name, SQL: query.SQL, Values: tc.values}
		for _, result := range query.ResultSets[0].Columns {
			entry.Columns = append(entry.Columns, column{result.ResultName(), semanticTypeName(result.Type)})
		}
		live = append(live, entry)
	}
	strictSQL := `SELECT Yson::SerializeJson(Yson::ParseJson("{")) AS malformed;`
	strict, err := analyzer.Analyze(nil, []model.Source{{Name: "document_strict.sql", Text: "-- name: Check :one\n" + strictSQL}})
	require.NoError(t, err)
	live = append(live, liveCase{Name: "document_strict", SQL: strict.Queries[0].SQL, ErrorText: "JSON"})
	for _, tc := range []struct{ sql, errorText string }{
		{`SELECT String::Base32Encode(42u) AS bad;`, "String"},
		{`DECLARE $text AS String; SELECT Unicode::IsAlpha($text) AS bad;`, "Utf8"},
		{`SELECT Url::GetHost(42u) AS bad;`, "String"},
		{`SELECT DateTime::Split("today") AS bad;`, "date/time"},
		{`SELECT Yson::From(CurrentUtcDate()) AS bad;`, "not supported"},
		{`SELECT Json::From(CurrentUtcDate()) AS bad;`, "not supported"},
		{`SELECT Roaring::Cardinality("not_bitmap") AS bad;`, "Resource"},
	} {
		_, err := analyzer.Analyze(nil, []model.Source{{Name: "invalid.sql", Text: "-- name: Check :one\n" + tc.sql}})
		assert.False(t, err == nil || !strings.Contains(err.Error(), tc.errorText), "%s: error = %v, want %q", tc.sql, err, tc.errorText)
	}
	if t.Failed() {
		t.FailNow()
	}
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING for live UDF validation")
	}
	payload, err := json.Marshal(live)
	require.NoError(t, err)
	const script = `import json, os, sys, urllib.parse
import ydb
cases = json.load(sys.stdin)
u = urllib.parse.urlsplit(os.environ["YDB_CONNECTION_STRING"])
def typename(t):
    if t.HasField("optional_type"): return "Optional<" + typename(t.optional_type.item) + ">"
    if t.HasField("type_id"): return t.DESCRIPTOR.fields_by_name["type_id"].enum_type.values_by_number[t.type_id].name.lower()
    raise AssertionError("unhandled server type: %s" % t)
def value(v):
    return v.decode("utf-8") if isinstance(v, bytes) else v
errors = []
with ydb.Driver(ydb.DriverConfig(u.scheme+"://"+u.netloc,u.path,credentials=ydb.AnonymousCredentials(),disable_discovery=True)) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        for case in cases:
            try:
                if case.get("error_text"):
                    try: pool.execute_with_retries(case["sql"])
                    except Exception as e:
                        assert case["error_text"].lower() in str(e).lower(), "error: %s" % e
                        print(case["name"]+": expected server error", flush=True)
                    else: raise AssertionError("expected server error")
                    continue
                result = pool.execute_with_retries(case["sql"])[0]
                actual_types = [{"name": c.name, "type": typename(c.type)} for c in result.columns]
                assert actual_types == case["columns"], "types: expected %s, got %s" % (case["columns"], actual_types)
                assert len(result.rows) == 1, "rows: %s" % result.rows
                actual_values = {c.name: value(result.rows[0][c.name]) for c in result.columns}
                assert actual_values == case["values"], "values: expected %s, got %s" % (case["values"], actual_values)
                print(case["name"]+": types and values match YDB", flush=True)
            except Exception as e: errors.append(case["name"]+": "+str(e))
assert not errors, "\n".join(errors)
`
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "live UDF validation: %v\n%s", err, out)
	t.Log(string(out))
}
