package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestImplicitProjectionNames(t *testing.T) {
	schema := `CREATE TABLE records (id Uint64 NOT NULL, column0 Uint64, PRIMARY KEY(id));`
	for _, tc := range []struct {
		sql   string
		names []string
	}{
		{`SELECT 1, 2 + 3, CAST(1 AS Uint64);`, []string{"column0", "column1", "column2"}},
		{`SELECT 1 AS z, 2;`, []string{"z", "column1"}},
		{`SELECT "Привет 😀 ?"u /* keep */, 2 AS Column0;`, []string{"column0", "Column0"}},
		{`SELECT 1, 2 AS column0;`, []string{"column0", "column1"}},
		{`SELECT 1 AS column2, 2, 3;`, []string{"column1", "column2", "column3"}},
		{`SELECT 1 AS z, 2 AS column2, 3, 4;`, []string{"column2", "column3", "column4", "z"}},
		{`SELECT (id), COALESCE(column0, 0ul) FROM records;`, []string{"id", "column1"}},
		{`SELECT a.column0, b.id, 1 FROM records AS a JOIN records AS b ON a.id=b.id;`, []string{"a.column0", "b.id", "column2"}},
		{`SELECT 1, a.column0 FROM records AS a JOIN records AS b ON a.id=b.id;`, []string{"column0", "a.column0"}},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			sql := "-- name: Read :many\n" + tc.sql
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: sql}})
			require.NoError(t, err)
			query := result.Queries[0]
			var names []string
			for _, column := range query.ResultSets[0].Columns {
				names = append(names, column.ResultName())
			}
			require.Equal(t, tc.names, names)
			require.Equal(t, sql, query.SQL)
		})
	}
}

func TestImplicitProjectionNamesSurviveWildcardExpansion(t *testing.T) {
	schema := `CREATE TABLE records (id Uint64 NOT NULL, column0 Uint64, PRIMARY KEY(id));`
	for _, tc := range []struct {
		projection, want string
		names            []string
	}{
		{"r.*, /* Привет 😀 */ COALESCE(column0, 7ul) /* tail */", "r.`id` AS `id`, `r`.`column0` AS `column0`, /* Привет 😀 */ COALESCE(column0, 7ul) AS `column1` /* tail */", []string{"id", "column0", "column1"}},
		{"COALESCE(column0, 7ul), r.*", "COALESCE(column0, 7ul) AS `column1`, r.`id` AS `id`, `r`.`column0` AS `column0`", []string{"column1", "id", "column0"}},
	} {
		t.Run(tc.projection, func(t *testing.T) {
			sql := "-- name: Read :many\nSELECT " + tc.projection + " FROM records AS r ORDER BY column1;"
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: sql}})
			require.NoError(t, err)
			query := result.Queries[0]
			want := "-- name: Read :many\nSELECT " + tc.want + " FROM records AS r ORDER BY column1;"
			require.Equal(t, want, query.SQL)
			var names []string
			for _, column := range query.ResultSets[0].Columns {
				names = append(names, column.ResultName())
			}
			require.Equal(t, tc.names, names)
			require.NotContains(t, query.SQL, "column2")
		})
	}
}
