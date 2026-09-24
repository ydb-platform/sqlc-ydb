package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeDMLReturning(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}}
	for _, statement := range []string{
		"INSERT INTO records (id, label) VALUES ($id, $label)",
		"UPDATE records SET label = $label WHERE id = $id",
		"DELETE FROM records WHERE id = $id AND label = $label",
	} {
		for _, projection := range []string{"id", "*"} {
			t.Run(strings.Fields(statement)[0]+"/"+projection, func(t *testing.T) {
				got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :one\n" + statement + " RETURNING " + projection + ";"}})
				require.NoError(t, err)
				want := got.Catalog.Tables[0].Columns
				if projection == "id" {
					want = want[:1]
				}
				q := got.Queries[0]
				require.Equal(t, model.One, q.Command)
				require.Len(t, q.ResultSets, 1)
				require.Equal(t, want, q.ResultSets[0].Columns)
				params := []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}}
				if strings.HasPrefix(statement, "UPDATE") {
					params[0], params[1] = params[1], params[0]
				}
				require.Equal(t, params, q.Parameters)
			})
		}
	}
}

func TestAnalyzeUpdateMultipleAssignments(t *testing.T) {
	got, err := Analyze([]model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Update :execrows\nUPDATE records SET label = $label, id = $id;"}})
	require.NoError(t, err)
	want := []model.Parameter{{Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}, {Name: "id", Type: model.Type{Kind: "Uint64"}}}
	q := got.Queries[0]
	require.Equal(t, model.ExecRows, q.Command)
	require.Len(t, q.ResultSets, 0)
	require.Equal(t, want, q.Parameters)
}

func TestAnalyzeRejectsUnsupportedQueryForms(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}}
	for _, tt := range []struct{ name, command, sql, want string }{
		{"tuple update", ":exec", "UPDATE records SET (id, label) = ($id, $label);", "only individual UPDATE SET assignments are supported"},
		{"exec select", ":exec", "SELECT id FROM records;", "command :exec cannot be used with a row-returning statement"},
		{"two data statements with row count", ":execrows", "DELETE FROM records; DELETE FROM records;", "multi-statement :execrows is unsupported; use :exec, :one, or :many"},
		{"query ddl", ":exec", "CREATE TABLE other (id Uint64, PRIMARY KEY (id));", "unsupported statement in named query"},
		{"join using", ":many", "SELECT a.id FROM records a JOIN records b USING (id);", "JOIN USING is not yet supported; use an explicit ON condition"},
		{"table function", ":many", "SELECT id FROM AS_TABLE($rows);", "requires DECLARE $rows AS List<Struct<...>>"},
		{"in subquery", ":many", "SELECT id FROM records WHERE id IN (SELECT r.id FROM records r UNION SELECT r.id FROM records r);", "CTEs, UNION and INTERSECT are unsupported"},
		{"array expression", ":one", "SELECT [1, 2] AS values;", "unsupported result expression"},
		{"exists expression", ":one", "SELECT EXISTS (SELECT id FROM records) AS present;", "unsupported result expression"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Invalid " + tt.command + "\n" + tt.sql}})
			require.ErrorContains(t, err, tt.want)
			require.NotNil(t, got)
			require.NotEqual(t, 0, len(got.Diagnostics))
			require.Equal(t, "query.sql", got.Diagnostics[0].Position.File)
		})
	}
}

func TestAnalyzeInsertMultipleRows(t *testing.T) {
	got, err := Analyze([]model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Insert :exec\nINSERT INTO records (id, label) VALUES ($first, $label), ($second, $label);"}})
	require.NoError(t, err)
	want := []model.Parameter{{Name: "first", Type: model.Type{Kind: "Uint64"}}, {Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}, {Name: "second", Type: model.Type{Kind: "Uint64"}}}
	q := got.Queries[0]
	require.Equal(t, model.Exec, q.Command)
	require.Len(t, q.ResultSets, 0)
	require.Equal(t, want, q.Parameters)
}

func TestAnalyzeDMLWithoutReturningRequiresExec(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records(id Uint64 NOT NULL,label Utf8,PRIMARY KEY(id));"}}
	for _, command := range []string{":one", ":many"} {
		for _, sql := range []string{
			"INSERT INTO records(id,label) VALUES(1ul,'value'u);",
			"UPSERT INTO records(id,label) VALUES(1ul,'value'u);",
			"UPDATE records SET label='value'u WHERE id=1ul;",
			"DELETE FROM records WHERE id=1ul;",
		} {
			t.Run(command+"/"+strings.Fields(sql)[0], func(t *testing.T) {
				result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change " + command + "\n" + sql}})
				want := model.Diagnostic{Position: model.Position{File: "query.sql", Line: 1, Column: 1}, Message: "command " + command + " requires a result set"}
				require.Error(t, err)
				require.Equal(t, []model.Diagnostic{want}, result.Diagnostics)
			})
		}
	}
}
