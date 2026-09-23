package analyzer

import (
	"reflect"
	"strings"
	"testing"

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
				if err != nil {
					t.Fatal(err)
				}
				want := got.Catalog.Tables[0].Columns
				if projection == "id" {
					want = want[:1]
				}
				q := got.Queries[0]
				if q.Command != model.One || len(q.ResultSets) != 1 || !reflect.DeepEqual(q.ResultSets[0].Columns, want) {
					t.Fatalf("query = %#v, want columns %#v", q, want)
				}
				params := []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}}
				if strings.HasPrefix(statement, "UPDATE") {
					params[0], params[1] = params[1], params[0]
				}
				if !reflect.DeepEqual(q.Parameters, params) {
					t.Fatalf("parameters = %#v, want %#v", q.Parameters, params)
				}
			})
		}
	}
}

func TestAnalyzeUpdateMultipleAssignments(t *testing.T) {
	got, err := Analyze([]model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Update :execrows\nUPDATE records SET label = $label, id = $id;"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Parameter{{Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}, {Name: "id", Type: model.Type{Kind: "Uint64"}}}
	q := got.Queries[0]
	if q.Command != model.ExecRows || len(q.ResultSets) != 0 || !reflect.DeepEqual(q.Parameters, want) {
		t.Fatalf("query = %#v, want parameters %#v", q, want)
	}
}

func TestAnalyzeRejectsUnsupportedQueryForms(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}}
	for _, tt := range []struct{ name, command, sql, want string }{
		{"tuple update", ":exec", "UPDATE records SET (id, label) = ($id, $label);", "only individual UPDATE SET assignments are supported"},
		{"exec select", ":exec", "SELECT id FROM records;", "command :exec cannot be used with a row-returning statement"},
		{"two data statements with row count", ":execrows", "DELETE FROM records; DELETE FROM records;", "multiple data statements require :exec"},
		{"query ddl", ":exec", "CREATE TABLE other (id Uint64, PRIMARY KEY (id));", "unsupported statement in named query"},
		{"join using", ":many", "SELECT a.id FROM records a JOIN records b USING (id);", "JOIN USING is not yet supported; use an explicit ON condition"},
		{"derived table", ":many", "SELECT id FROM (SELECT id FROM records) r;", "only named catalog tables are supported in FROM and JOIN"},
		{"table function", ":many", "SELECT id FROM AS_TABLE($rows);", "requires DECLARE $rows AS List<Struct<...>>"},
		{"in subquery", ":many", "SELECT id FROM records WHERE id IN (SELECT r.id FROM records r UNION SELECT r.id FROM records r);", "CTEs, UNION and INTERSECT are unsupported"},
		{"array expression", ":one", "SELECT [1, 2] AS values;", "unsupported result expression"},
		{"exists expression", ":one", "SELECT EXISTS (SELECT id FROM records) AS present;", "unsupported result expression"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Invalid " + tt.command + "\n" + tt.sql}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if got == nil || len(got.Diagnostics) == 0 || got.Diagnostics[0].Position.File != "query.sql" {
				t.Fatalf("missing query diagnostic: %#v", got)
			}
		})
	}
}

func TestAnalyzeInsertMultipleRows(t *testing.T) {
	got, err := Analyze([]model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Insert :exec\nINSERT INTO records (id, label) VALUES ($first, $label), ($second, $label);"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Parameter{{Name: "first", Type: model.Type{Kind: "Uint64"}}, {Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}, {Name: "second", Type: model.Type{Kind: "Uint64"}}}
	q := got.Queries[0]
	if q.Command != model.Exec || len(q.ResultSets) != 0 || !reflect.DeepEqual(q.Parameters, want) {
		t.Fatalf("query = %#v, want parameters %#v", q, want)
	}
}
