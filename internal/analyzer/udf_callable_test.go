package analyzer

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestCallableUDFInNamedBinding(t *testing.T) {
	for _, sql := range []string{
		"$re = Pire::Grep(\"a\"); SELECT $re(\"cat\") AS matched;",
		"SELECT Pire::Grep(\"a\")(\"cat\") AS matched;",
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Match :one\n" + sql}})
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if got := result.Queries[0].ResultSets[0].Columns[0].Type.String(); got != "Bool" {
			t.Fatalf("%s: type = %s", sql, got)
		}
	}
}

func TestCallableUDFRequiresInvocation(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Match :one\nSELECT Pire::Grep(\"a\") AS matcher;"}})
	if err == nil || !strings.Contains(err.Error(), "nonpersistable type Callable") {
		t.Fatalf("error = %v", err)
	}
}

func TestDateTimeCallableUDF(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"$format = DateTime::Format(\"%Y-%m-%d\"); SELECT $format(CurrentUtcTimestamp()) AS formatted;", "String"},
		{"SELECT DateTime::Format(\"%Y-%m-%d\")(CurrentUtcTimestamp()) AS formatted;", "String"},
		{"$parse = DateTime::Parse(\"%Y-%m-%d\"); SELECT DateTime::MakeTimestamp($parse(\"2026-09-24\")) AS parsed;", "Optional<Timestamp>"},
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + tc.sql}})
		if err != nil {
			t.Fatalf("%s: %v", tc.sql, err)
		}
		if got := result.Queries[0].ResultSets[0].Columns[0].Type.String(); got != tc.want {
			t.Fatalf("%s: type = %s, want %s", tc.sql, got, tc.want)
		}
	}
}

func TestYsonConvertToTypeArgument(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT Yson::ConvertTo(Yson::ParseJson(\"[\\\"x\\\"]\"), List<String>) AS values;", "Optional<List<String>>"},
		{"SELECT Yson::ConvertTo(Yson::ParseJson(\"123\"), Int64) AS value;", "Optional<Int64>"},
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + tc.sql}})
		if err != nil {
			t.Fatalf("%s: %v", tc.sql, err)
		}
		if got := result.Queries[0].ResultSets[0].Columns[0].Type.String(); got != tc.want {
			t.Fatalf("%s: type = %s, want %s", tc.sql, got, tc.want)
		}
	}
}

func TestHistogramUDFWithAggregate(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE metrics (id Uint64 NOT NULL, amount Double, PRIMARY KEY (id));"}}
	query := []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT Histogram::Print(HISTOGRAM(amount), 50) AS summary FROM metrics;"}}
	result, err := Analyze(schema, query)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Queries[0].ResultSets[0].Columns[0].Type.String(); got != "Optional<String>" {
		t.Fatalf("type = %s, want Optional<String>", got)
	}
}

func TestLiteralDependentRegexResult(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT Pire::MultiMatch(@@a\nb@@)(\"a\") AS matches;", "Tuple<Bool,Bool>"},
		{"SELECT Re2::Capture(\"(?P<foo>x)(a)\")(\"xa\").foo AS capture;", "Optional<String>"},
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\n" + tc.sql}})
		if err != nil {
			t.Fatalf("%s: %v", tc.sql, err)
		}
		if got := result.Queries[0].ResultSets[0].Columns[0].Type.String(); got != tc.want {
			t.Fatalf("%s: type = %s, want %s", tc.sql, got, tc.want)
		}
	}
}

func TestUnicodeStringLiteralCoercion(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT Unicode::GetLength(\"жніўня\") AS length;", "Uint64"},
		{"SELECT Unicode::SplitToList(\"One, two\", \", \") AS words;", "List<Utf8>"},
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\n" + tc.sql}})
		if err != nil {
			t.Fatalf("%s: %v", tc.sql, err)
		}
		if got := result.Queries[0].ResultSets[0].Columns[0].Type.String(); got != tc.want {
			t.Fatalf("%s: type = %s, want %s", tc.sql, got, tc.want)
		}
	}
}
