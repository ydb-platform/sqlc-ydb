package analyzer

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestBatchInsertSelect(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Json, PRIMARY KEY(id));`}}
	for _, source := range []string{"SELECT id, label FROM AS_TABLE($values)", "SELECT r.id, r.label FROM AS_TABLE($values) AS r"} {
		sql := "-- name: CreateRecords :exec\nDECLARE $values AS List<Struct<id:Uint64,label:Json>>;\nINSERT INTO records (id,label) " + source + ";"
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		if err != nil {
			t.Fatal(err)
		}
		q := result.Queries[0]
		if len(q.Parameters) != 1 || q.Parameters[0].Type.Elem.Kind != "Struct" || len(q.Parameters[0].Type.Elem.Fields) != 2 {
			t.Fatalf("parameters: %#v", q.Parameters)
		}
		if q.SQL != sql || strings.Contains(q.SQLWithoutDeclarations, "DECLARE") {
			t.Fatalf("SQL preservation: %#v", q)
		}
	}
}

func TestBatchInsertDiagnostics(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Json, PRIMARY KEY(id));`}}
	for _, tt := range []struct{ decl, sql, want string }{
		{"List<Struct<id:Utf8,label:Json>>", "INSERT INTO records (id,label) SELECT id,label FROM AS_TABLE($books)", "requires Uint64"},
		{"List<Struct<id:Uint64?,label:Json>>", "INSERT INTO records (id,label) SELECT id,label FROM AS_TABLE($books)", "requires Uint64"},
		{"List<Struct<id:Uint64,label:Json>>", "INSERT INTO records (id,label) SELECT id,missing FROM AS_TABLE($books)", "unknown column"},
		{"List<Struct<id:Uint64,label:Json>>", "INSERT INTO records (missing,label) SELECT id,label FROM AS_TABLE($books)", "unknown column"},
		{"List<Struct<id:Uint64,label:Json>>", "INSERT INTO records (id,label) SELECT id FROM AS_TABLE($books)", "1 columns for 2"},
		{"List<Struct<id:Uint64,label:Json>>", "INSERT INTO records (id,id) SELECT id,id FROM AS_TABLE($books)", "duplicate target column"},
		{"List<Uint64>", "INSERT INTO records (id) SELECT id FROM AS_TABLE($books)", "requires DECLARE"},
		{"List<Struct<id:Uint64,id:Json>>", "INSERT INTO records (id) SELECT id FROM AS_TABLE($books)", "duplicate Struct field"},
	} {
		t.Run(tt.want+tt.decl, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: CreateRecords :exec\nDECLARE $books AS " + tt.decl + ";\n" + tt.sql + ";"}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want %s", err, tt.want)
			}
		})
	}
}

func TestBatchInsertExplicitSourceColumns(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Json, PRIMARY KEY(id));`}}
	for _, projection := range []string{"*", "r.*"} {
		sql := "-- name: CreateRecords :exec\nDECLARE $books AS List<Struct<label:Json,id:Uint64>>; INSERT INTO records (id,label) SELECT " + projection + " FROM AS_TABLE($books) r;"
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		if err == nil || !strings.Contains(err.Error(), "requires explicit source columns") {
			t.Fatalf("%s: %v", projection, err)
		}
	}
	// Declaration order and aliases do not change the explicit SELECT mapping.
	sql := "-- name: CreateRecords :exec\nDECLARE $books AS List<Struct<label:Json,id:Uint64>>; INSERT INTO records (id,label) SELECT r.id AS another_id,r.label AS another_label FROM AS_TABLE($books) AS r;"
	if _, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}}); err != nil {
		t.Fatal(err)
	}
}

func TestBatchStructQuotedFieldNames(t *testing.T) {
	for _, quote := range []string{"`", "\"", "'"} {
		sql := "-- name: ReadRows :many\nDECLARE $books AS List<Struct<" + quote + "book:id" + quote + ":Uint64,label:Json>>; SELECT r.`book:id`, r.label FROM AS_TABLE($books) r;"
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
		if err != nil {
			t.Fatalf("%s: %v", quote, err)
		}
		if result.Queries[0].Parameters[0].Type.Elem.Fields[0].Name != "book:id" {
			t.Fatalf("name=%q", result.Queries[0].Parameters[0].Type.Elem.Fields[0].Name)
		}
	}
}

func TestBatchStructRejectsDynamicFieldName(t *testing.T) {
	sql := "-- name: ReadRows :many\nDECLARE $books AS List<Struct<$name:Uint64>>; SELECT id FROM AS_TABLE($books);"
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	if err == nil || !strings.Contains(err.Error(), "unsupported Struct field") {
		t.Fatalf("err=%v", err)
	}
}
