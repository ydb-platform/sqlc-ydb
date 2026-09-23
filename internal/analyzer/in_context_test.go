package analyzer

import (
	"strings"
	"testing"

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
			if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), tc.context) {
				t.Fatalf("error = %v; want explicit IN context boundary in %s", err, tc.name)
			}
			if len(result.Queries) != 0 {
				t.Fatal("unsupported IN expression produced an analyzed query")
			}
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
			if err != nil {
				t.Fatal(err)
			}
			analyzed := result.Queries[0]
			if analyzed.SQL != query || len(analyzed.Parameters) != 1 || analyzed.Parameters[0].Name != "ids" || analyzed.Parameters[0].Type.String() != "List<Uint64>" {
				t.Fatalf("IN predicate changed SQL or parameter type: %+v", analyzed)
			}
			columns := analyzed.ResultSets[0].Columns
			if len(columns) != 1 || columns[0].Type.String() != "Uint64" {
				t.Fatalf("IN predicate changed result type: %+v", columns)
			}
		})
	}
}
