package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestMultiResultSetsRetainOrderNamesAndTypes(t *testing.T) {
	const sql = "-- name: Summary :multi\nDECLARE $id AS Uint64;\n-- result: Count\nSELECT $id AS value;\n-- result: Status\nSELECT false AS value;\nSELECT \"2\"u AS value;"
	result, err := Analyze(nil, []model.Source{{Name: "queries.sql", Text: sql}})
	require.NoError(t, err)
	require.Len(t, result.Queries, 1)
	q := result.Queries[0]
	require.Equal(t, sql, q.SQL)
	require.Equal(t, model.Multi, q.Command)
	require.Equal(t, []string{"Count", "Status", "Result3"}, []string{q.ResultSets[0].Name, q.ResultSets[1].Name, q.ResultSets[2].Name})
	require.Equal(t, []string{"Uint64", "Bool", "Utf8"}, []string{q.ResultSets[0].Columns[0].Type.Kind, q.ResultSets[1].Columns[0].Type.Kind, q.ResultSets[2].Columns[0].Type.Kind})
	require.Equal(t, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, q.Parameters)
	require.Equal(t, []string{"id"}, q.DeclaredParameters)
	require.True(t, q.MultipleStatements)
}

func TestMultiResultAnnotationDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, sql, want string }{
		{"one result", "SELECT 1 AS value;", ":multi requires at least two top-level SELECT statements"},
		{"one DML", "DELETE FROM records;", ":multi supports only top-level SELECT statements"},
		{"DML", "SELECT 1 AS value; DELETE FROM records; SELECT 2 AS value;", ":multi supports only top-level SELECT statements"},
		{"collision with default", "-- result: Result2\nSELECT 1 AS value; SELECT 2 AS value;", "result name \"Result2\" is used more than once"},
		{"duplicate annotation", "-- result: First\n-- result: Second\nSELECT 1 AS value; SELECT 2 AS value;", "result annotation must immediately precede a top-level SELECT"},
		{"orphan annotation", "SELECT 1 AS value; SELECT 2 AS value;\n-- result: Orphan", "result annotation must immediately precede a top-level SELECT"},
		{"invalid name", "-- result: bad-name\nSELECT 1 AS value; SELECT 2 AS value;", "invalid result name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "queries.sql", Text: "-- name: Summary :multi\n" + tc.sql}})
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMultiResultMarkersIgnoreStringAndBlockComment(t *testing.T) {
	const sql = "-- name: Summary :multi\nSELECT \"-- result: Hidden\"u AS value;\n/* -- result: Hidden */ SELECT false AS value;"
	result, err := Analyze(nil, []model.Source{{Name: "queries.sql", Text: sql}})
	require.NoError(t, err)
	require.Equal(t, []string{"Result1", "Result2"}, []string{result.Queries[0].ResultSets[0].Name, result.Queries[0].ResultSets[1].Name})
}

func TestMultiResultUnicodeNameAndWildcard(t *testing.T) {
	const sql = "-- name: Read :multi\n-- result: Имя\nSELECT * FROM records;\nSELECT false AS enabled;"
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, name Utf8, PRIMARY KEY(id));"}}
	result, err := Analyze(schema, []model.Source{{Name: "queries.sql", Text: sql}})
	require.NoError(t, err)
	query := result.Queries[0]
	require.Equal(t, []string{"Имя", "Result2"}, []string{query.ResultSets[0].Name, query.ResultSets[1].Name})
	require.Equal(t, []string{"id", "name"}, []string{query.ResultSets[0].Columns[0].Name, query.ResultSets[0].Columns[1].Name})
	require.Equal(t, "Bool", query.ResultSets[1].Columns[0].Type.Kind)
	require.Contains(t, query.SQL, "-- result: Имя\nSELECT `id`, `name` FROM records;")
}
