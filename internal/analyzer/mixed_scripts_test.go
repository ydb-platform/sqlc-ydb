package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestMixedScriptPreservesOneResultInEveryPosition(t *testing.T) {
	for _, command := range []model.Command{model.One, model.Many} {
		for _, selectSQL := range []string{
			"SELECT id, payload FROM records WHERE id=$id;",
			"SELECT id, payload FROM records WHERE id=$id UNION ALL SELECT id, payload FROM copies WHERE id=$id;",
		} {
			for _, statements := range []string{
				selectSQL + " DELETE FROM copies WHERE id=$id;",
				"DELETE FROM copies WHERE id=$id; " + selectSQL,
				"UPDATE records SET payload='new'u WHERE id=$id; " + selectSQL + " DELETE FROM copies WHERE id=$id;",
			} {
				t.Run(string(command)+statements, func(t *testing.T) {
					sql := "-- name: Change " + string(command) + "\n" + statements
					result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
					require.NoError(t, err)
					require.Len(t, result.Queries, 1)
					q := result.Queries[0]
					require.Equal(t, sql, q.SQL)
					require.Equal(t, command, q.Command)
					require.Len(t, q.ResultSets, 1)
					require.True(t, q.MultipleStatements)
					got := q.ResultSets[0].Columns
					require.Len(t, got, 2)
					require.Equal(t, "id", got[0].Name)
					require.Equal(t, "Uint64", got[0].Type.Kind)
					require.Equal(t, "payload", got[1].Name)
					require.Equal(t, "Utf8", got[1].Type.Kind)
					want := []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}
					require.Equal(t, want, q.Parameters)
				})
			}
		}
	}
}

func TestMixedScriptRejectsResultCountAndCommand(t *testing.T) {
	for _, tc := range []struct{ name, command, sql, want string }{
		{"two selects", ":many", "SELECT id FROM records; SELECT id FROM copies;", "multi-statement queries support at most one result-producing statement; found 2"},
		{"two results with writes", ":one", "DELETE FROM records; SELECT id FROM records; DELETE FROM copies RETURNING id;", "multi-statement queries support at most one result-producing statement; found 2"},
		{"exec result", ":exec", "SELECT id FROM records; DELETE FROM copies;", "command :exec cannot be used with a row-returning script; use :one or :many"},
		{"one no result", ":one", "DELETE FROM records; DELETE FROM copies;", "command :one requires exactly one result-producing statement in a script"},
		{"many no result", ":many", "DELETE FROM records; DELETE FROM copies;", "command :many requires exactly one result-producing statement in a script"},
		{"each writes", ":each", "SELECT id FROM records; DELETE FROM copies;", "multi-statement :each is unsupported; use :one or :many to consume the result before returning"},
		{"execrows", ":execrows", "SELECT id FROM records; DELETE FROM copies;", "multi-statement :execrows is unsupported; use :exec, :one, or :many"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change " + tc.command + "\n" + tc.sql}})
			require.Error(t, err)
			require.Len(t, result.Diagnostics, 1)
			require.Equal(t, tc.want, result.Diagnostics[0].Message)
		})
	}
}

func TestMixedScriptWildcardAndEmptyResultMetadata(t *testing.T) {
	const sql = "-- name: Change :many\nDECLARE $id AS Uint64;\nUPSERT INTO copies SELECT * FROM records;\nSELECT r.* FROM records AS r WHERE false;\nDELETE FROM copies WHERE id=$id;"
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	q := result.Queries[0]
	require.Len(t, q.ResultSets, 1)
	require.Len(t, q.ResultSets[0].Columns, 2)
	require.Len(t, q.Syntax.Selects, 2)
	require.NotContains(t, q.SQL, "SELECT *")
	require.NotContains(t, q.SQL, "r.*")
	require.True(t, strings.HasSuffix(q.SQL, "DELETE FROM copies WHERE id=$id;"))
}

func TestMixedScriptDatabaseValidatesOriginalSQLOnce(t *testing.T) {
	const sql = "-- name: Change :many\nPRAGMA TablePathPrefix='/local/tenant';\nDECLARE $id AS Uint64;\nUPDATE records SET name='changed'u WHERE id=$id;\nSELECT * FROM records WHERE id=$id;\nDELETE FROM copies WHERE id=$id;"
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"/local/tenant/records": databaseTestTable("name"), "/local/tenant/copies": databaseTestTable("name")}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
	require.NoError(t, err)
	require.Equal(t, []string{sql}, database.validated)
	require.Equal(t, []string{"/local/tenant/records", "/local/tenant/copies"}, database.described)
	require.Len(t, result.Queries, 1)
	require.Len(t, result.Queries[0].ResultSets, 1)
	require.Equal(t, "/local/tenant/records", result.Queries[0].ResultSets[0].Columns[0].Table)
}

func TestMixedScriptRejectsLateParameterRefinementOfResult(t *testing.T) {
	schema := append([]model.Source{}, dmlScriptSchema...)
	schema = append(schema, model.Source{Name: "loose.sql", Text: "CREATE TABLE loose(id Uint64 NOT NULL,payload Utf8,PRIMARY KEY(id));"})
	const read = "SELECT $p AS projected FROM loose WHERE payload=$p;"
	const write = "UPDATE records SET payload=$p;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :many\n" + read + write}})
	const want = "parameter $p changes inferred type from Optional<Utf8> to Utf8 after the result statement; add DECLARE before the script to keep its result type stable"
	require.Error(t, err)
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, want, result.Diagnostics[0].Message)
	for _, sql := range []string{"DECLARE $p AS Utf8; " + read + write, write + read} {
		result, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :many\n" + sql}})
		require.NoError(t, err)
		q := result.Queries[0]
		require.Len(t, q.Parameters, 1)
		require.Equal(t, "Utf8", q.Parameters[0].Type.Kind)
		require.Equal(t, "Utf8", q.ResultSets[0].Columns[0].Type.Kind)
	}
	sql := "UPDATE loose SET payload=$p; SELECT id FROM copies WHERE id=$id; " + write
	result, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :many\n" + sql}})
	require.NoError(t, err)
	wantParameters := []model.Parameter{{Name: "p", Type: model.Type{Kind: "Utf8"}}, {Name: "id", Type: model.Type{Kind: "Uint64"}}}
	require.Equal(t, "Uint64", result.Queries[0].ResultSets[0].Columns[0].Type.Kind)
	require.Equal(t, wantParameters, result.Queries[0].Parameters)
}

func TestMixedScriptRejectsIncompatibleSharedParameters(t *testing.T) {
	for _, sql := range []string{
		"SELECT id FROM records WHERE id=$id; DELETE FROM texts WHERE id=$id;",
		"DELETE FROM texts WHERE id=$id; SELECT id FROM records WHERE id=$id;",
	} {
		result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :many\n" + sql}})
		require.Error(t, err)
		found := false
		for _, d := range result.Diagnostics {
			if d.Message == "external parameter $id has incompatible inferred types; add DECLARE to specify its intended type" {
				found = true
			}
		}
		require.True(t, found)
	}
}

func TestMixedScriptReturningPreservesOneResult(t *testing.T) {
	for _, returning := range []string{
		"INSERT INTO records(id,payload) VALUES($id,'new'u) RETURNING *;",
		"UPSERT INTO records(id,payload) VALUES($id,'new'u) RETURNING *;",
		"UPDATE records SET payload='new'u WHERE id=$id RETURNING *;",
		"DELETE FROM records WHERE id=$id RETURNING *;",
	} {
		for _, sql := range []string{returning + "DELETE FROM copies WHERE id=$id;", "DELETE FROM copies WHERE id=$id;" + returning, "DELETE FROM texts;" + returning + "DELETE FROM copies WHERE id=$id;"} {
			for _, command := range []string{":one", ":many"} {
				result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change " + command + "\n" + sql}})
				require.NoError(t, err)
				q := result.Queries[0]
				require.Len(t, q.ResultSets, 1)
				require.Equal(t, result.Catalog.Tables[0].Columns, q.ResultSets[0].Columns)
				require.Contains(t, q.SQL, "RETURNING `id`, `payload`")
			}
		}
	}
}

func TestMixedScriptReturningTypeDoesNotDependOnParameterRefinement(t *testing.T) {
	schema := append([]model.Source{}, dmlScriptSchema...)
	schema = append(schema, model.Source{Name: "loose.sql", Text: "CREATE TABLE loose(id Uint64 NOT NULL,payload Utf8,PRIMARY KEY(id));"})
	const sql = "-- name: Change :many\nUPDATE loose SET payload=$p RETURNING payload; UPDATE records SET payload=$p;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	q := result.Queries[0]
	require.Len(t, q.Parameters, 1)
	require.Equal(t, "Utf8", q.Parameters[0].Type.Kind)
	require.True(t, q.ResultSets[0].Columns[0].Type.Equal(model.Optional(model.Type{Kind: "Utf8"})))
}

func TestMixedScriptUsesNamedTabularBindings(t *testing.T) {
	const sql = "-- name: Change :many\n$selection=(SELECT id FROM records); DELETE FROM copies; SELECT id FROM $selection;"
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	{
		q := result.Queries[0]
		require.Equal(t, sql, q.SQL)
		require.True(t, q.MultipleStatements)
		require.Len(t, q.ResultSets, 1)
		require.Len(t, q.ResultSets[0].Columns, 1)
		require.Equal(t, "Uint64", q.ResultSets[0].Columns[0].Type.Kind)
	}
}

func TestMixedScriptInferenceGuardPreservesSingleQueryTypes(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE loose(id Uint64 NOT NULL,payload Utf8,PRIMARY KEY(id));"}}
	for _, tc := range []struct {
		sql string
		typ model.Type
	}{
		{"SELECT $p AS projected FROM loose WHERE payload=$p;", model.Optional(model.Type{Kind: "Utf8"})},
		{"DECLARE $p AS Utf8; SELECT $p AS projected FROM loose WHERE payload=$p;", model.Type{Kind: "Utf8"}},
		{"DECLARE $p AS Utf8?; $local=$p; SELECT $local AS projected FROM loose WHERE payload=$local;", model.Optional(model.Type{Kind: "Utf8"})},
	} {
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.sql}})
		require.NoError(t, err)
		q := result.Queries[0]
		require.False(t, q.MultipleStatements)
		require.Len(t, q.Parameters, 1)
		require.Equal(t, "p", q.Parameters[0].Name)
		require.True(t, q.Parameters[0].Type.Equal(tc.typ))
		require.Len(t, q.ResultSets, 1)
		require.True(t, q.ResultSets[0].Columns[0].Type.Equal(tc.typ))
	}
}

func TestMixedScriptUnresolvedResultParameterKeepsOriginalDiagnostic(t *testing.T) {
	schema := append([]model.Source{}, dmlScriptSchema...)
	schema = append(schema, model.Source{Name: "loose.sql", Text: "CREATE TABLE loose(id Uint64 NOT NULL,payload Utf8,PRIMARY KEY(id));"})
	for _, projection := range []string{"$p AS projected", "id, $p AS projected"} {
		t.Run(projection, func(t *testing.T) {
			sql := "-- name: Change :many\nSELECT " + projection + " FROM loose; UPDATE records SET payload=$p;"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
			const want = "cannot resolve type of parameter $p; add DECLARE"
			require.Error(t, err)
			require.Len(t, result.Diagnostics, 1)
			require.Equal(t, want, result.Diagnostics[0].Message)
		})
	}
}
