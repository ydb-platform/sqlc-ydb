package analyzer

import (
	"context"
	"reflect"
	"strings"
	"testing"

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
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Queries) != 1 {
		t.Fatalf("queries = %d", len(result.Queries))
	}
	q := result.Queries[0]
	want := []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "payload", Type: model.Type{Kind: "Utf8"}}}
	if !q.MultipleStatements || q.SQL != sql || len(q.ResultSets) != 0 || !reflect.DeepEqual(q.Parameters, want) || !reflect.DeepEqual(q.DeclaredParameters, []string{"id", "payload"}) {
		t.Fatalf("query = %#v", q)
	}
	tables := map[string]bool{}
	for _, name := range q.Syntax.Tables {
		tables[name] = true
	}
	if !tables["records"] || !tables["copies"] || len(q.Syntax.Tables) != 5 {
		t.Fatalf("physical references = %#v", q.Syntax.Tables)
	}
	bound := map[string]bool{}
	for _, binding := range q.Syntax.Columns {
		bound[binding.Table] = true
	}
	if !bound["records"] || !bound["copies"] {
		t.Fatalf("column bindings = %#v", q.Syntax.Columns)
	}
}

func TestDMLScriptInferredParametersUseStatementScope(t *testing.T) {
	for _, sql := range []string{
		"DELETE FROM records WHERE id = $number; DELETE FROM texts WHERE id = $text; DELETE FROM copies WHERE id = $number;",
		"INSERT INTO records (id,payload) VALUES ($number,$text); UPDATE copies SET payload=$text WHERE id=$number; DELETE FROM texts WHERE id=$text;",
	} {
		result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\n" + sql}})
		if err != nil {
			t.Fatal(err)
		}
		want := []model.Parameter{{Name: "number", Type: model.Type{Kind: "Uint64"}}, {Name: "text", Type: model.Type{Kind: "Utf8"}}}
		if !reflect.DeepEqual(result.Queries[0].Parameters, want) {
			t.Fatalf("parameters = %#v", result.Queries[0].Parameters)
		}
	}
}

func TestDMLScriptRejectsConflictingInferredParameters(t *testing.T) {
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\nDELETE FROM records WHERE id=$id; DELETE FROM texts WHERE id=$id;"}})
	const want = "external parameter $id has incompatible inferred types; add DECLARE to specify its intended type"
	if err == nil {
		t.Fatal("accepted conflicting parameter types")
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Message == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v, want %q", result.Diagnostics, want)
	}
}

func TestDMLScriptSelectSourcesAndWildcardNormalization(t *testing.T) {
	const sql = "-- name: Change :exec\nDECLARE $rows AS List<Struct<id:Uint64,payload:Utf8>>;\nUPSERT INTO records SELECT r.* FROM AS_TABLE($rows) AS r;\nUPDATE copies ON SELECT r.* FROM AS_TABLE($rows) AS r;\nDELETE FROM records WHERE id IN (SELECT r.id FROM AS_TABLE($rows) AS r);\nDELETE FROM copies ON SELECT id FROM records;"
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	q := result.Queries[0]
	if strings.Contains(q.SQL, "r.*") || strings.Count(q.SQL, "AS `payload`") != 2 || len(q.Syntax.Selects) != 4 || len(q.Parameters) != 1 {
		t.Fatalf("query = %#v; SQL=%s", q, q.SQL)
	}
	for token, binding := range q.Syntax.Columns {
		if token < 0 || binding.Column.Name == "" {
			t.Fatalf("invalid normalized binding = %d: %#v", token, binding)
		}
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
			if err == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != tc.want {
				t.Fatalf("diagnostics = %#v; error = %v; want %q", result.Diagnostics, err, tc.want)
			}
		})
	}
}

func TestDMLScriptDatabaseDiscoversAllTargetsAndValidatesOnce(t *testing.T) {
	const sql = "-- name: Change :exec\nPRAGMA TablePathPrefix='/local/tenant';\nDECLARE $id AS Uint64;\nDECLARE $name AS Utf8;\nINSERT INTO records(id,name) VALUES($id,$name);\nUPSERT INTO copies SELECT * FROM records;\nUPDATE copies SET name=$name WHERE copies.id=$id;\nDELETE FROM records WHERE records.id=$id;"
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"/local/tenant/records": databaseTestTable("name"), "/local/tenant/copies": databaseTestTable("name")}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(database.validated, []string{sql}) || !reflect.DeepEqual(database.described, []string{"/local/tenant/records", "/local/tenant/copies"}) {
		t.Fatalf("validated=%q described=%q", database.validated, database.described)
	}
	if len(result.Queries) != 1 || strings.Contains(result.Queries[0].SQL, "SELECT *") {
		t.Fatalf("queries=%#v", result.Queries)
	}
	for _, name := range result.Queries[0].Syntax.Tables {
		if !strings.HasPrefix(name, "/local/tenant/") {
			t.Fatalf("unresolved table: %q", name)
		}
	}
}

func TestDMLScriptRejectsForwardLocalReference(t *testing.T) {
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\n$first=$later; $later=1ul; DELETE FROM records WHERE id=$first; DELETE FROM copies;"}})
	if err == nil || len(result.Diagnostics) == 0 || result.Diagnostics[0].Message != "cannot resolve local $first from $later; declare the external parameter first" {
		t.Fatalf("diagnostics=%#v; error=%v", result.Diagnostics, err)
	}
}

func TestDMLScriptSharedParametersRefineNullability(t *testing.T) {
	schema := append([]model.Source{}, dmlScriptSchema...)
	schema = append(schema, model.Source{Name: "loose.sql", Text: "CREATE TABLE loose (id Uint64 NOT NULL, payload Utf8, PRIMARY KEY(id));"})
	const optional = "UPDATE loose SET payload=$p;"
	const required = "UPDATE records SET payload=$p || \"!\"u WHERE payload=$p;"
	for _, sql := range []string{optional + required, required + optional, optional + "UPDATE records SET payload=$p; UPDATE copies SET payload=$p || \"!\"u;"} {
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\n" + sql}})
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		want := []model.Parameter{{Name: "p", Type: model.Type{Kind: "Utf8"}}}
		if !reflect.DeepEqual(result.Queries[0].Parameters, want) {
			t.Fatalf("parameters=%#v", result.Queries[0].Parameters)
		}
	}
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\nDECLARE $p AS Utf8?; " + optional + required}})
	if err == nil || !strings.Contains(err.Error(), "cannot assign Optional<Utf8> to column \"payload\" of type Utf8") {
		t.Fatalf("declared optional type was overwritten: %v", err)
	}
}

func TestDMLScriptUnknownColumnKeepsStatementPosition(t *testing.T) {
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\nDELETE FROM records WHERE payload='value'u;\nDELETE FROM texts WHERE payload='value'u;"}})
	if err == nil || len(result.Diagnostics) == 0 {
		t.Fatalf("accepted column from preceding table: %v", err)
	}
	d := result.Diagnostics[0]
	if d.Message != "unknown column \"payload\"" || d.Position.File != "query.sql" || d.Position.Line != 3 {
		t.Fatalf("diagnostic=%#v", d)
	}
}
