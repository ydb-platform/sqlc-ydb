package analyzer

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func TestRowNumberWindows(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		`SELECT id, ROW_NUMBER() OVER () AS row_num FROM records;`,
		`SELECT id, ROW_NUMBER() OVER (PARTITION BY category ORDER BY id) AS row_num FROM records;`,
		`SELECT id, ROW_NUMBER() OVER w AS row_num FROM records WINDOW w AS (PARTITION BY category ORDER BY id);`,
		`SELECT id, ROW_NUMBER() OVER (ORDER BY id DESC) AS row_num FROM records;`,
		`SELECT r.id, ROW_NUMBER() OVER (PARTITION BY r.category ORDER BY r.id) AS row_num FROM records AS r;`,
		`SELECT id, (ROW_NUMBER() OVER (ORDER BY id)) AS row_num FROM records;`,
		`SELECT category, ROW_NUMBER() OVER w AS row_num FROM records GROUP BY category WINDOW w AS (ORDER BY category);`,
	} {
		t.Run(statement, func(t *testing.T) {
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: RankRecords :many\n" + statement}})
			require.NoError(t, err)
			require.Equal(t, model.Type{Kind: "Uint64"}, result.Queries[0].ResultSets[0].Columns[1].Type)
			require.Equal(t, "row_num", result.Queries[0].ResultSets[0].Columns[1].Name)
		})
	}
}

func TestRowNumberWindowOrderUsesSourceBinding(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8, PRIMARY KEY(id));`}}
	query := `SELECT category AS id, ROW_NUMBER() OVER w AS row_num FROM records WINDOW w AS (ORDER BY id);`
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: RankRecords :many\n" + query}})
	require.NoError(t, err)
	var order parser.IWindow_order_clauseContext
	descendants(result.Queries[0].Syntax.Root, func(node antlr.Tree) {
		if window, ok := node.(parser.IWindow_order_clauseContext); ok {
			order = window
		}
	})
	require.NotNil(t, order)
	ref := columnRefs(order.Order_by_clause().Sort_specification_list().Sort_specification(0).Expr())[0]
	binding := result.Queries[0].Syntax.Columns[ref.ctx.GetStart().GetTokenIndex()]
	require.Equal(t, "id", binding.Column.Name)
}

func TestRowNumberInTupleINSubquery(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8, PRIMARY KEY(id));`}}
	query := `SELECT id FROM records WHERE (id,id) IN (SELECT (id, ROW_NUMBER() OVER w) FROM records WINDOW w AS (ORDER BY id));`
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: RankRecords :many\n" + query}})
	require.NoError(t, err)
}

func TestRowNumberWindowDiagnostics(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8, PRIMARY KEY(id));`}}
	for _, tc := range []struct{ statement, want string }{
		{`SELECT ROW_NUMBER() AS row_num FROM records;`, "requires OVER"},
		{`SELECT ROW_NUMBER(id) OVER (ORDER BY id) AS row_num FROM records;`, "no arguments"},
		{`SELECT ROW_NUMBER(*) OVER (ORDER BY id) AS row_num FROM records;`, "no arguments"},
		{`SELECT ROW_NUMBER() OVER missing AS row_num FROM records;`, "unknown window"},
		{`SELECT ROW_NUMBER() OVER w AS row_num FROM records WINDOW w AS (ORDER BY id), w AS (ORDER BY category);`, "duplicate window"},
		{`SELECT ROW_NUMBER() OVER (PARTITION BY absent ORDER BY id) AS row_num FROM records;`, "unknown column"},
		{`SELECT ROW_NUMBER() OVER (PARTITION BY id + 1 ORDER BY id) AS row_num FROM records;`, "PARTITION BY requires direct columns"},
		{`SELECT ROW_NUMBER() OVER (PARTITION BY id AS key ORDER BY id) AS row_num FROM records;`, "PARTITION BY requires direct columns"},
		{`SELECT ROW_NUMBER() OVER (PARTITION COMPACT BY category ORDER BY id) AS row_num FROM records;`, "PARTITION COMPACT"},
		{`SELECT ROW_NUMBER() OVER (ORDER BY absent) AS row_num FROM records;`, "unknown column"},
		{`SELECT ROW_NUMBER() OVER (ORDER BY id + 1) AS row_num FROM records;`, "window ORDER BY requires direct columns"},
		{`SELECT ROW_NUMBER() OVER (ORDER BY id ROWS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING) AS row_num FROM records;`, "window frames"},
		{`SELECT ROW_NUMBER() OVER child AS row_num FROM records WINDOW base AS (ORDER BY id), child AS (base);`, "inherited window"},
		{`SELECT ROW_NUMBER() OVER (w) AS row_num FROM records WINDOW w AS (ORDER BY id);`, "inherited window"},
		{`SELECT SUM(id) OVER (ORDER BY id) AS total FROM records;`, "window function"},
		{`SELECT ROW_NUMBER() OVER () AS row_num;`, "FROM source"},
		{`SELECT id FROM records WHERE ROW_NUMBER() OVER (ORDER BY id) = 1;`, "SELECT projection"},
		{`$rank = ROW_NUMBER() OVER (); SELECT $rank AS row_num FROM records;`, "SELECT projection"},
	} {
		t.Run(tc.statement, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: RankRecords :many\n" + tc.statement}})
			require.ErrorContains(t, err, tc.want)
		})
	}
}
