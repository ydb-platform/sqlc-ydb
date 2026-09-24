package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestCallableUDFInNamedBinding(t *testing.T) {
	for _, sql := range []string{
		"$re = Pire::Grep(\"a\"); SELECT $re(\"cat\") AS matched;",
		"SELECT Pire::Grep(\"a\")(\"cat\") AS matched;",
		"SELECT Pire::Grep(\"a\"s)(\"cat\"s) AS matched;",
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Match :one\n" + sql}})
		require.NoError(t, err)
		{
			got := result.Queries[0].ResultSets[0].Columns[0].Type.String()
			require.Equal(t, "Bool", got)
		}
	}
}

func TestBoundCallableStructResult(t *testing.T) {
	const sql = "-- name: Capture :one\n$capture = Re2::Capture(\"(?P<word>x)\"); SELECT $capture(\"x\").word AS value;"
	result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	{
		got := result.Queries[0].ResultSets[0].Columns[0].Type.String()
		require.Equal(t, "Optional<String>", got)
	}
}

func TestCallableInvocationDiagnostics(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT $missing(\"cat\") AS value;", "unknown callable binding $missing"},
		{"$value = 1; SELECT $value(\"cat\") AS value;", "is not callable"},
		{"$re = Pire::Grep(\"a\"); SELECT $re() AS value;", "expects 1 arguments, got 0"},
		{"$re = Pire::Grep(\"a\"); SELECT $re(1) AS value;", "callable argument 1 has type"},
		{"$re = Pire::Grep(\"a\"); SELECT $re(Unknown::Call(\"cat\")) AS value;", "unsupported YQL function \"Unknown::Call\""},
		{"$re = Pire::Grep(\"a\"); SELECT $re(\"cat\" AS text) AS value;", "does not support named arguments"},
		{"SELECT Pire::Grep(\"a\")(\"cat\")() AS value;", "is not callable"},
	} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Match :one\n" + tc.sql}})
		require.ErrorContains(t, err, tc.want)
	}
}

func TestCallableUDFRequiresInvocation(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Match :one\nSELECT Pire::Grep(\"a\") AS matcher;"}})
	require.ErrorContains(t, err, "nonpersistable type Callable")
}

func TestDateTimeCallableUDF(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"$format = DateTime::Format(\"%Y-%m-%d\"); SELECT $format(CurrentUtcTimestamp()) AS formatted;", "String"},
		{"SELECT DateTime::Format(\"%Y-%m-%d\")(CurrentUtcTimestamp()) AS formatted;", "String"},
		{"$parse = DateTime::Parse(\"%Y-%m-%d\"); SELECT DateTime::MakeTimestamp($parse(\"2026-09-24\")) AS parsed;", "Optional<Timestamp>"},
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + tc.sql}})
		require.NoError(t, err)
		{
			got := result.Queries[0].ResultSets[0].Columns[0].Type.String()
			require.Equal(t, tc.want, got)
		}
	}
}

func TestYsonConvertToTypeArgument(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT Yson::ConvertTo(Yson::ParseJson(\"[\\\"x\\\"]\"), List<String>) AS values;", "Optional<List<String>>"},
		{"SELECT Yson::ConvertTo(Yson::ParseJson(\"123\"), Int64) AS value;", "Optional<Int64>"},
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + tc.sql}})
		require.NoError(t, err)
		{
			got := result.Queries[0].ResultSets[0].Columns[0].Type.String()
			require.Equal(t, tc.want, got)
		}
	}
}

func TestYsonConvertToTypeArgumentInTabularProjection(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8 NOT NULL, PRIMARY KEY (id));"}}
	query := []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT Yson::ConvertTo(Json::From(AGGREGATE_LIST(label)), List<String>) AS values FROM records;"}}
	result, err := Analyze(schema, query)
	require.NoError(t, err)
	{
		got := result.Queries[0].ResultSets[0].Columns[0].Type.String()
		require.Equal(t, "List<String>", got)
	}
}

func TestYsonConvertToRequiresLiteralPositionalTargetType(t *testing.T) {
	for _, sql := range []string{
		"SELECT Yson::ConvertTo(Yson::ParseJson(\"{}\"), 1) AS value;",
		"SELECT Yson::ConvertTo(Yson::ParseJson(\"{}\"), Uint64 AS Target) AS value;",
	} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\n" + sql}})
		require.Error(t, err)
		require.False(t, !strings.Contains(err.Error(), "target type") && !strings.Contains(err.Error(), "literal YQL target type"))
	}
}

func TestListCreateRequiresLiteralElementType(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT ListCreate(Uint64 AS ElementType) AS values;"}})
	require.ErrorContains(t, err, "ListCreate expects one literal YQL element type")
}

func TestHistogramUDFWithAggregate(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE metrics (id Uint64 NOT NULL, amount Double, PRIMARY KEY (id));"}}
	query := []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT Histogram::Print(HISTOGRAM(amount), 50) AS summary FROM metrics;"}}
	result, err := Analyze(schema, query)
	require.NoError(t, err)
	{
		got := result.Queries[0].ResultSets[0].Columns[0].Type.String()
		require.Equal(t, "Optional<String>", got)
	}
}

func TestLiteralDependentRegexResult(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT Pire::MultiMatch(@@a\nb@@)(\"a\") AS matches;", "Tuple<Bool,Bool>"},
		{"SELECT Re2::Capture(\"(?P<foo>x)(a)\")(\"xa\").foo AS capture;", "Optional<String>"},
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\n" + tc.sql}})
		require.NoError(t, err)
		{
			got := result.Queries[0].ResultSets[0].Columns[0].Type.String()
			require.Equal(t, tc.want, got)
		}
	}
}

func TestUnicodeStringLiteralCoercion(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT Unicode::GetLength(\"жніўня\") AS length;", "Uint64"},
		{"SELECT Unicode::SplitToList(\"One, two\", \", \") AS words;", "List<Utf8>"},
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\n" + tc.sql}})
		require.NoError(t, err)
		{
			got := result.Queries[0].ResultSets[0].Columns[0].Type.String()
			require.Equal(t, tc.want, got)
		}
	}
}
