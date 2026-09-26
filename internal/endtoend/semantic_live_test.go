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

// Verify server column names, order and full types independently of renderers.
// Run only against an explicitly supplied disposable test database.
func TestLiveYDBSemanticTypes(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for live semantic validation")
	}
	table := fmt.Sprintf("sqlc_semantic_%d", time.Now().UnixNano())
	schema := "CREATE TABLE " + table + " (id Uint64 NOT NULL, flag Bool NOT NULL, n Int32 NOT NULL, f Float NOT NULL, maybe Int32, label Utf8, stamp Timestamp NOT NULL, amount Decimal(22,9), PRIMARY KEY(id));"
	queries := []struct{ Name, SQL string }{
		{"wildcard", "SELECT * FROM $TABLE;"},
		{"qualified_wildcard", "SELECT r.* FROM $TABLE AS r;"},
		{"joined_wildcard", "SELECT b.* FROM $TABLE AS a LEFT JOIN $TABLE AS b ON a.id=b.id;"},
		{"without_wildcard", "SELECT * WITHOUT maybe, label FROM $TABLE;"},
		{"without_qualified_wildcard", "SELECT b.* WITHOUT a.label, b.maybe FROM $TABLE AS a LEFT JOIN $TABLE AS b ON a.id=b.id;"},
		{"without_if_exists", "SELECT * WITHOUT IF EXISTS absent, maybe, maybe FROM $TABLE;"},
		{"without_explicit_projection", "SELECT n WITHOUT absent FROM $TABLE;"},
		{"union_wildcard", "SELECT * FROM $TABLE UNION ALL SELECT * FROM $TABLE;"},
		{"arithmetic", "SELECT (n + 2) * 3 - 4 AS precedence, maybe + 1 AS nullable, n + id AS mixed, f * n AS floating FROM $TABLE;"},
		{"arithmetic_widths", "SELECT 1t + 2t AS i8, 1s * 2s AS i16, 1 - 2 AS i32, 1l + 2 AS i64, 1ut + 2ut AS u8, 1us * 2us AS u16, 1u - 2u AS u32, 1ul + 2ul AS u64, 1u + 2 AS mixed32, 1ul + 2l AS mixed64, 1l + 2.0f AS float_value, 1.0f + 2.0 AS double_value;"},
		{"casts", "SELECT CAST(n AS Int64) AS wide, CAST(n AS Uint8) AS narrow, CAST(f AS Int32) AS integer_value FROM $TABLE;"},
		{"case", "SELECT CASE WHEN flag THEN n ELSE maybe END AS choice, CASE n WHEN 1 THEN 1u ELSE 2 END AS mixed FROM $TABLE;"},
		{"coalesce_explicit_numeric", "SELECT COALESCE(CAST(n AS Int64), 1l) AS required, COALESCE(CAST(maybe AS Int64), 1l) AS fallback FROM $TABLE;"},
		{"substring_positions", "SELECT SUBSTRING(\"abc\", CAST(n AS Uint8)) AS small, SUBSTRING(\"abc\", CAST(n AS Uint16)) AS medium, SUBSTRING(\"abc\", CAST(id AS Uint32)) AS wide FROM $TABLE;"},
		{"find_positions", "SELECT FIND(\"abc\", \"a\", CAST(n AS Uint16)) AS first, RFIND(\"abc\", \"a\", CAST(id AS Uint32)) AS last FROM $TABLE;"},
		{"union_join_missing", "SELECT a.id FROM $TABLE AS a JOIN $TABLE AS b ON a.id=b.id UNION ALL SELECT b.id FROM $TABLE AS a JOIN $TABLE AS b ON a.id=b.id;"},
		{"union_join_names", "SELECT a.id, b.id FROM $TABLE AS a JOIN $TABLE AS b ON a.id=b.id UNION ALL SELECT a.id, b.id FROM $TABLE AS a JOIN $TABLE AS b ON a.id=b.id;"},
		{"coalesce", "SELECT COALESCE(maybe, n) AS value, LENGTH(COALESCE(label, \"fallback\"u)) AS size FROM $TABLE;"},
		{"aggregate", "SELECT COUNT(*) AS count, SUM(n) AS total, AVG(f) AS mean, MIN(f) AS minimum, MAX(maybe) AS maximum FROM $TABLE;"},
		{"group", "SELECT id, SUM(n) AS total, AVG(f) AS mean, MIN(f) AS minimum, MAX(maybe) AS maximum FROM $TABLE GROUP BY id HAVING COUNT(*) > 0ul;"},
		{"group_nested", "SELECT id, COALESCE(SUM(n), 0l) AS total FROM $TABLE GROUP BY id;"},
		{"case_comparison", "SELECT CASE WHEN n > 1 THEN maybe ELSE n END AS value FROM $TABLE;"},
		{"if_comparison", "SELECT IF(n = 1, maybe, n) AS value FROM $TABLE;"},
		{"union_new_order", "SELECT 1 AS z UNION ALL SELECT 2 AS y, 3 AS a;"},
		{"union_three_arms", "SELECT 1 AS z UNION ALL SELECT 2 AS y, 3 AS d UNION ALL SELECT 4 AS b, 5 AS a;"},
		{"union_order", "SELECT 1 AS z, 2 AS a UNION ALL SELECT 3 AS a, 4 AS z;"},
		{"union_prefix", "SELECT 1 AS id, 2 AS z, 3 AS a UNION ALL SELECT 4 AS id, 5 AS a, 6 AS z;"},
		{"union_missing", "SELECT 1 AS z UNION ALL SELECT 2 AS a;"},
		{"union_names", "SELECT 1 AS z, 2 AS b UNION ALL SELECT 3 AS a, 4 AS b;"},
		{"union_types", "SELECT n AS value FROM $TABLE UNION SELECT maybe AS value FROM $TABLE;"},
		{"mixed_integers", "SELECT CASE WHEN TRUE THEN 1l ELSE 1ul END AS wide, CASE WHEN TRUE THEN 1 ELSE 1u END AS small;"},
		{"libraries", "SELECT String::AsciiToLower(\"ABC\") AS lower, Unicode::GetLength(\"текст\"u) AS size;"},
	}
	queries = append(queries, builtinLiveQueries()...)
	queries = append(queries, digestLiveQueries()...)
	queries = append(queries, struct{ Name, SQL string }{"collection_predicate", `SELECT SetIsDisjoint(ToSet(Yson::ConvertToStringList(CAST(NULL AS Json?))), Yson::ConvertToStringList(CAST(NULL AS Json?))) AS disjoint, Yson::ConvertToStringList(CAST(NULL AS Yson?)) AS empty_strings, SetIsDisjoint(ToSet(CAST(NULL AS List<String>?)), CAST(NULL AS List<String>?)) AS nullable_disjoint;`})
	queries = append(queries, struct{ Name, SQL string }{"decimal_aggregates", "SELECT SUM(amount) AS total, AVG(amount) AS mean FROM $TABLE;"})
	type column struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type liveCase struct {
		Name    string   `json:"name"`
		SQL     string   `json:"sql"`
		Columns []column `json:"columns"`
	}
	cases := make([]liveCase, 0, len(queries))
	for _, q := range queries {
		sql := strings.ReplaceAll(q.SQL, "$TABLE", table)
		a, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: q.Name + ".sql", Text: "-- name: Check :many\n" + sql}})
		if err != nil {
			assert.Fail(t, fmt.Sprintf("%s: %v", q.Name, err))
			continue
		}
		c := liveCase{Name: q.Name, SQL: a.Queries[0].SQL}
		for _, col := range a.Queries[0].ResultSets[0].Columns {
			c.Columns = append(c.Columns, column{col.ResultName(), semanticTypeName(col.Type)})
		}
		cases = append(cases, c)
	}
	if t.Failed() {
		t.FailNow()
	}
	payload, err := json.Marshal(struct {
		Schema, Table string
		Cases         []liveCase
	}{schema, table, cases})
	require.NoError(t, err)
	const script = `import json, os, sys, urllib.parse
import ydb
payload = json.load(sys.stdin)
u = urllib.parse.urlsplit(os.environ["YDB_CONNECTION_STRING"])
def typename(t):
    if t.HasField("optional_type"): return "Optional<" + typename(t.optional_type.item) + ">"
    if t.HasField("list_type"): return "List<" + typename(t.list_type.item) + ">"
    if t.HasField("decimal_type"): return "Decimal(%s,%s)" % (t.decimal_type.precision, t.decimal_type.scale)
    if t.HasField("type_id"): return t.DESCRIPTOR.fields_by_name["type_id"].enum_type.values_by_number[t.type_id].name.lower()
    raise AssertionError("unhandled server type: %s" % t)
errors = []
with ydb.Driver(ydb.DriverConfig(u.scheme+"://"+u.netloc,u.path,credentials=ydb.AnonymousCredentials(),disable_discovery=True)) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        pool.execute_with_retries(payload["Schema"])
        try:
            for case in payload["Cases"]:
                try:
                    result = pool.execute_with_retries(case["sql"])[0]
                    actual = [{"name": c.name, "type": typename(c.type)} for c in result.columns]
                    assert actual == case["columns"], "expected %s; got %s" % (case["columns"], actual)
                    print(case["name"]+": matches YDB", flush=True)
                except Exception as e: errors.append(case["name"]+": "+str(e))
        finally: pool.execute_with_retries("DROP TABLE "+payload["Table"]+";")
assert not errors, "\n".join(errors)
`
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "live semantic validation: %v\n%s", err, out)
	t.Log(string(out))
}

func digestLiveQueries() []struct{ Name, SQL string } {
	return []struct{ Name, SQL string }{
		{"digest_cityhash", `SELECT Digest::CityHash("abc") AS plain, Digest::CityHash(CAST(NULL AS String?)) AS null_input, Digest::CityHash("abc", NULL AS Init) AS seed_null, Digest::CityHash("abc", 42ul AS Init) AS seeded;`},
		{"digest_crc64", `SELECT Digest::Crc64("abc") AS plain, Digest::Crc64(CAST(NULL AS String?)) AS null_input, Digest::Crc64("abc", NULL AS Init) AS seed_null, Digest::Crc64("abc", 42ul AS Init) AS seeded;`},
		{"digest_fnv64", `SELECT Digest::Fnv64("abc") AS plain, Digest::Fnv64(CAST(NULL AS String?)) AS null_input, Digest::Fnv64("abc", NULL AS Init) AS seed_null, Digest::Fnv64("abc", 42ul AS Init) AS seeded;`},
		{"digest_murmur_hash", `SELECT Digest::MurMurHash("abc") AS plain, Digest::MurMurHash(CAST(NULL AS String?)) AS null_input, Digest::MurMurHash("abc", NULL AS Init) AS seed_null, Digest::MurMurHash("abc", 42ul AS Init) AS seeded;`},
		{"digest_murmur_hash_2a", `SELECT Digest::MurMurHash2A("abc") AS plain, Digest::MurMurHash2A(CAST(NULL AS String?)) AS null_input, Digest::MurMurHash2A("abc", NULL AS Init) AS seed_null, Digest::MurMurHash2A("abc", 42ul AS Init) AS seeded;`},
		{"digest_fnv32", `SELECT Digest::Fnv32("abc") AS plain, Digest::Fnv32(CAST(NULL AS String?)) AS null_input, Digest::Fnv32("abc", NULL AS Init) AS seed_null, Digest::Fnv32("abc", 42u AS Init) AS seeded;`},
		{"digest_murmur_hash32", `SELECT Digest::MurMurHash32("abc") AS plain, Digest::MurMurHash32(CAST(NULL AS String?)) AS null_input, Digest::MurMurHash32("abc", NULL AS Init) AS seed_null, Digest::MurMurHash32("abc", 42u AS Init) AS seeded;`},
		{"digest_murmur_hash_2a32", `SELECT Digest::MurMurHash2A32("abc") AS plain, Digest::MurMurHash2A32(CAST(NULL AS String?)) AS null_input, Digest::MurMurHash2A32("abc", NULL AS Init) AS seed_null, Digest::MurMurHash2A32("abc", 42u AS Init) AS seeded;`},
		{"digest_crc32c", `SELECT Digest::Crc32c("abc") AS plain, Digest::Crc32c(CAST(NULL AS String?)) AS null_input;`},
		{"digest_farm_hash_fingerprint32", `SELECT Digest::FarmHashFingerprint32("abc") AS plain, Digest::FarmHashFingerprint32(CAST(NULL AS String?)) AS null_input;`},
		{"digest_super_fast_hash", `SELECT Digest::SuperFastHash("abc") AS plain, Digest::SuperFastHash(CAST(NULL AS String?)) AS null_input;`},
		{"digest_md5_hex", `SELECT Digest::Md5Hex("abc") AS plain, Digest::Md5Hex(CAST(NULL AS String?)) AS null_input;`},
		{"digest_md5_raw", `SELECT Digest::Md5Raw("abc") AS plain, Digest::Md5Raw(CAST(NULL AS String?)) AS null_input;`},
		{"digest_sha1", `SELECT Digest::Sha1("abc") AS plain, Digest::Sha1(CAST(NULL AS String?)) AS null_input;`},
		{"digest_sha256", `SELECT Digest::Sha256("abc") AS plain, Digest::Sha256(CAST(NULL AS String?)) AS null_input;`},
		{"digest_md5_half_mix", `SELECT Digest::Md5HalfMix("abc") AS plain, Digest::Md5HalfMix(CAST(NULL AS String?)) AS null_input;`},
		{"digest_farm_hash_fingerprint64", `SELECT Digest::FarmHashFingerprint64("abc") AS plain, Digest::FarmHashFingerprint64(CAST(NULL AS String?)) AS null_input;`},
		{"digest_xxh3", `SELECT Digest::XXH3("abc") AS plain, Digest::XXH3(CAST(NULL AS String?)) AS null_input;`},
		{"digest_numeric_hash", `SELECT Digest::NumericHash(42ul) AS plain, Digest::NumericHash(CAST(NULL AS Uint64?)) AS null_input;`},
		{"digest_farm_hash_fingerprint", `SELECT Digest::FarmHashFingerprint(42ul) AS plain, Digest::FarmHashFingerprint(CAST(NULL AS Uint64?)) AS null_input;`},
		{"digest_int_hash64", `SELECT Digest::IntHash64(42ul) AS plain, Digest::IntHash64(CAST(NULL AS Uint64?)) AS null_input;`},
	}
}

func TestDigestLiveQueriesAnalyzeOffline(t *testing.T) {
	for _, probe := range digestLiveQueries() {
		t.Run(probe.Name, func(t *testing.T) {
			result, err := analyzer.Analyze(nil, []model.Source{{Name: probe.Name + ".sql", Text: "-- name: Check :one\n" + probe.SQL}})
			require.NoError(t, err)
			require.Len(t, result.Queries, 1)
			require.Len(t, result.Queries[0].ResultSets, 1)
			require.GreaterOrEqual(t, len(result.Queries[0].ResultSets[0].Columns), 2)
		})
	}
}

func semanticTypeName(typ model.Type) string {
	switch typ.Kind {
	case "Optional", "List":
		return typ.Kind + "<" + semanticTypeName(*typ.Elem) + ">"
	case "Decimal":
		return fmt.Sprintf("Decimal(%d,%d)", typ.Precision, typ.Scale)
	default:
		return strings.ToLower(typ.Kind)
	}
}
