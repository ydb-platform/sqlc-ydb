package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

var dmlScriptSchema = []model.Source{{Name: "schema.sql", Text: `
CREATE TABLE records (id Uint64 NOT NULL, payload Utf8 NOT NULL, PRIMARY KEY(id));
CREATE TABLE copies (id Uint64 NOT NULL, payload Utf8 NOT NULL, PRIMARY KEY(id));
CREATE TABLE texts (id Utf8 NOT NULL, PRIMARY KEY(id));
`}}

func TestDMLScriptPreservesOneQueryAndSharedBindings(t *testing.T) {
	const sql = "-- name: Change :exec\r\nDECLARE $id AS Uint64;\r\nDECLARE $payload AS Utf8;\r\n$local = $id;\r\n-- Keep '; SELECT' and Unicode: пример\r\nINSERT INTO records (id, payload) VALUES ($local, $payload);\r\nUPSERT INTO copies SELECT id, payload FROM records WHERE id = $local;\r\nUPDATE records SET payload = $payload WHERE records.id = $id;\r\nDELETE FROM copies WHERE copies.id = $id;"
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	require.Len(t, result.Queries, 1)
	q := result.Queries[0]
	want := []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "payload", Type: model.Type{Kind: "Utf8"}}}
	require.True(t, q.MultipleStatements)
	require.Equal(t, sql, q.SQL)
	require.Len(t, q.ResultSets, 0)
	require.Equal(t, want, q.Parameters)
	require.Equal(t, []string{"id", "payload"}, q.DeclaredParameters)
	tables := map[string]bool{}
	for _, name := range q.Syntax.Tables {
		tables[name] = true
	}
	require.True(t, tables["records"])
	require.True(t, tables["copies"])
	require.Len(t, q.Syntax.Tables, 5)
	bound := map[string]bool{}
	for _, binding := range q.Syntax.Columns {
		bound[binding.Table] = true
	}
	require.True(t, bound["records"])
	require.True(t, bound["copies"])
}

func TestDMLScriptInferredParametersUseStatementScope(t *testing.T) {
	for _, sql := range []string{
		"DELETE FROM records WHERE id = $number; DELETE FROM texts WHERE id = $text; DELETE FROM copies WHERE id = $number;",
		"INSERT INTO records (id,payload) VALUES ($number,$text); UPDATE copies SET payload=$text WHERE id=$number; DELETE FROM texts WHERE id=$text;",
	} {
		result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\n" + sql}})
		require.NoError(t, err)
		want := []model.Parameter{{Name: "number", Type: model.Type{Kind: "Uint64"}}, {Name: "text", Type: model.Type{Kind: "Utf8"}}}
		require.Equal(t, want, result.Queries[0].Parameters)
	}
}

func TestDMLScriptRejectsConflictingInferredParameters(t *testing.T) {
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\nDELETE FROM records WHERE id=$id; DELETE FROM texts WHERE id=$id;"}})
	const want = "external parameter $id has incompatible inferred types; add DECLARE to specify its intended type"
	require.Error(t, err)
	found := false
	for _, d := range result.Diagnostics {
		if d.Message == want {
			found = true
		}
	}
	require.True(t, found)
}

func TestDMLScriptSelectSourcesAndWildcardNormalization(t *testing.T) {
	const sql = "-- name: Change :exec\nDECLARE $rows AS List<Struct<id:Uint64,payload:Utf8>>;\nUPSERT INTO records SELECT r.* FROM AS_TABLE($rows) AS r;\nUPDATE copies ON SELECT r.* FROM AS_TABLE($rows) AS r;\nDELETE FROM records WHERE id IN (SELECT r.id FROM AS_TABLE($rows) AS r);\nDELETE FROM copies ON SELECT id FROM records;"
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	q := result.Queries[0]
	require.NotContains(t, q.SQL, "r.*")
	require.Equal(t, 2, strings.Count(q.SQL, "AS `payload`"))
	require.Len(t, q.Syntax.Selects, 4)
	require.Len(t, q.Parameters, 1)
	for token, binding := range q.Syntax.Columns {
		require.False(t, token < 0)
		require.NotEqual(t, "", binding.Column.Name)
	}
}

func TestDMLScriptRejectsUnsupportedShapes(t *testing.T) {
	for _, tc := range []struct{ name, command, sql, want string }{
		{"rows command", ":execrows", "DELETE FROM records; DELETE FROM copies;", "multi-statement :execrows is unsupported; use :exec, :one, or :many"},
		{"row command", ":many", "DELETE FROM records; DELETE FROM copies;", "command :many requires exactly one result-producing statement in a script"},
		{"select before", ":exec", "SELECT id FROM records; DELETE FROM records;", "command :exec cannot be used with a row-returning script; use :one or :many"},
		{"select after", ":exec", "DELETE FROM records; SELECT id FROM records;", "command :exec cannot be used with a row-returning script; use :one or :many"},
		{"insert returning", ":exec", "INSERT INTO records(id,payload) VALUES(1ul,'a'u) RETURNING id; DELETE FROM copies;", "command :exec cannot be used with a row-returning script; use :one or :many"},
		{"update returning", ":exec", "UPDATE records SET payload='a'u RETURNING id; DELETE FROM copies;", "command :exec cannot be used with a row-returning script; use :one or :many"},
		{"returning", ":exec", "DELETE FROM records RETURNING id; DELETE FROM copies;", "command :exec cannot be used with a row-returning script; use :one or :many"},
		{"late declaration", ":exec", "DELETE FROM records; DECLARE $id AS Uint64; DELETE FROM copies WHERE id=$id;", "DECLARE and local assignments must precede all data statements in a script"},
		{"declaration after local", ":exec", "$local=$id; DECLARE $id AS Uint64; DELETE FROM records WHERE id=$local; DELETE FROM copies;", "DECLARE statements must precede local assignments in a script"},
		{"late local", ":exec", "DECLARE $id AS Uint64; DELETE FROM records; $local=$id; DELETE FROM copies WHERE id=$local;", "DECLARE and local assignments must precede all data statements in a script"},
		{"unsupported command", ":exec", "DELETE FROM records; COMMIT; DELETE FROM copies;", "unsupported statement in named query: \"COMMIT\""},
		{"no data", ":exec", "DECLARE $id AS Uint64;", "named query requires a SELECT, INSERT/UPSERT, UPDATE, or DELETE statement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change " + tc.command + "\n" + tc.sql}})
			require.Error(t, err)
			require.Len(t, result.Diagnostics, 1)
			require.Equal(t, tc.want, result.Diagnostics[0].Message)
		})
	}
}

func TestDMLScriptDatabaseDiscoversAllTargetsAndValidatesOnce(t *testing.T) {
	const sql = "-- name: Change :exec\nPRAGMA TablePathPrefix='/local/tenant';\nDECLARE $id AS Uint64;\nDECLARE $name AS Utf8;\nINSERT INTO records(id,name) VALUES($id,$name);\nUPSERT INTO copies SELECT * FROM records;\nUPDATE copies SET name=$name WHERE copies.id=$id;\nDELETE FROM records WHERE records.id=$id;"
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"/local/tenant/records": databaseTestTable("name"), "/local/tenant/copies": databaseTestTable("name")}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
	require.NoError(t, err)
	require.Equal(t, []string{sql}, database.validated)
	require.Equal(t, []string{"/local/tenant/records", "/local/tenant/copies"}, database.described)
	require.Len(t, result.Queries, 1)
	require.NotContains(t, result.Queries[0].SQL, "SELECT *")
	for _, name := range result.Queries[0].Syntax.Tables {
		require.True(t, strings.HasPrefix(name, "/local/tenant/"))
	}
}

func TestDMLScriptRejectsForwardLocalReference(t *testing.T) {
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\n$first=$later; $later=1ul; DELETE FROM records WHERE id=$first; DELETE FROM copies;"}})
	require.Error(t, err)
	require.NotEqual(t, 0, len(result.Diagnostics))
	require.Equal(t, "cannot resolve local $first from $later; declare the external parameter first", result.Diagnostics[0].Message)
}

func TestDMLScriptSharedParametersRefineNullability(t *testing.T) {
	schema := append([]model.Source{}, dmlScriptSchema...)
	schema = append(schema, model.Source{Name: "loose.sql", Text: "CREATE TABLE loose (id Uint64 NOT NULL, payload Utf8, PRIMARY KEY(id));"})
	const optional = "UPDATE loose SET payload=$p;"
	const required = "UPDATE records SET payload=$p || \"!\"u WHERE payload=$p;"
	for _, sql := range []string{optional + required, required + optional, optional + "UPDATE records SET payload=$p; UPDATE copies SET payload=$p || \"!\"u;"} {
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\n" + sql}})
		require.NoError(t, err)
		want := []model.Parameter{{Name: "p", Type: model.Type{Kind: "Utf8"}}}
		require.Equal(t, want, result.Queries[0].Parameters)
	}
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\nDECLARE $p AS Utf8?; " + optional + required}})
	require.ErrorContains(t, err, "cannot assign Optional<Utf8> to column \"payload\" of type Utf8")
}

func TestDMLScriptUnknownColumnKeepsStatementPosition(t *testing.T) {
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\nDELETE FROM records WHERE payload='value'u;\nDELETE FROM texts WHERE payload='value'u;"}})
	require.Error(t, err)
	require.NotEqual(t, 0, len(result.Diagnostics))
	d := result.Diagnostics[0]
	require.Equal(t, "unknown column \"payload\"", d.Message)
	require.Equal(t, "query.sql", d.Position.File)
	require.Equal(t, 3, d.Position.Line)
}
