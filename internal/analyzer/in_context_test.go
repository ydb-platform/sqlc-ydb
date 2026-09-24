package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestTypedINReportsUnsupportedExpressionContext(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	const want = "typed IN expressions are supported only in WHERE and JOIN predicates; they are not yet supported in projections, CASE, IF, or HAVING"
	for _, tc := range []struct{ name, statement, context string }{
		{"having", "SELECT id FROM records GROUP BY id HAVING id IN $ids;", "cannot resolve HAVING expression"},
		{"projection", "SELECT id IN $ids AS selected FROM records;", ""},
		{"case", "SELECT CASE WHEN id IN $ids THEN 1u ELSE 0u END AS selected FROM records;", "cannot resolve CASE condition"},
		{"if", "SELECT IF(id IN $ids, 1u, 0u) AS selected FROM records;", "cannot resolve argument of IF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := "-- name: Read :many\nDECLARE $ids AS List<Uint64>;\n" + tc.statement
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.Error(t, err)
			require.Contains(t, err.Error(), want)
			require.Contains(t, err.Error(), tc.context)
			require.Len(t, result.Queries, 0)
		})
	}
}

func TestTypedINRemainsSupportedInPredicateContexts(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		"SELECT id FROM records WHERE id IN $ids;",
		"SELECT a.id FROM records AS a JOIN records AS b ON a.id = b.id AND a.id IN $ids;",
	} {
		t.Run(statement, func(t *testing.T) {
			query := "-- name: Read :many\nDECLARE $ids AS List<Uint64>;\n" + statement
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			analyzed := result.Queries[0]
			require.Equal(t, query, analyzed.SQL)
			require.Len(t, analyzed.Parameters, 1)
			require.Equal(t, "ids", analyzed.Parameters[0].Name)
			require.Equal(t, "List<Uint64>", analyzed.Parameters[0].Type.String())
			columns := analyzed.ResultSets[0].Columns
			require.Len(t, columns, 1)
			require.Equal(t, "Uint64", columns[0].Type.String())
		})
	}
}
