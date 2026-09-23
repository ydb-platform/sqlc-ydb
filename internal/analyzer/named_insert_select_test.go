package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestNamedInsertSelect(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, note Utf8, PRIMARY KEY(id));"}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		for _, projection := range []string{"r.label, r.id", "*", "r.*", "r.*, NULL AS note", "1ul AS id, r.label"} {
			t.Run(verb+"/"+projection, func(t *testing.T) {
				query := "-- name: Write :exec\nDECLARE $rows AS List<Struct<label:Utf8,id:Uint64,>>;\n" + verb + " INTO records SELECT " + projection + " FROM AS_TABLE($rows) AS r;"
				got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
				if err != nil {
					t.Fatal(err)
				}
				wantRows := model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{
					{Name: "label", Type: model.Type{Kind: "Utf8"}},
					{Name: "id", Type: model.Type{Kind: "Uint64"}},
				}}}
				if !reflect.DeepEqual(got.Queries[0].Parameters, []model.Parameter{{Name: "rows", Type: wantRows}}) {
					t.Fatalf("parameters: %#v", got.Queries[0].Parameters)
				}
				sql := got.Queries[0].SQL
				if strings.Contains(sql, "*") {
					t.Fatalf("wildcard was not expanded: %s", sql)
				}
				if !strings.Contains(sql, verb+" INTO records SELECT ") {
					t.Fatalf("named mapping changed: %s", sql)
				}
			})
		}
	}
}

func TestNamedInsertSelectTableSources(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `
CREATE TABLE records (id Uint64 NOT NULL, label Utf8, note Utf8, PRIMARY KEY(id));
CREATE TABLE source_records (label Utf8 NOT NULL, id Uint32 NOT NULL, PRIMARY KEY(id));
CREATE TABLE source_notes (id Uint32 NOT NULL, text Utf8, PRIMARY KEY(id));`}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		for _, tt := range []struct{ name, selectSQL, expandedSQL string }{
			{"reordered columns", "SELECT label, id FROM source_records", "SELECT label, id FROM source_records"},
			{"wildcard", "SELECT * FROM source_records", "SELECT `label`, `id` FROM source_records"},
			{"qualified wildcard", "SELECT r.* FROM source_records AS r", "SELECT r.`label` AS `label`, `r`.`id` AS `id` FROM source_records AS r"},
			{"mixed wildcard and alias", "SELECT r.*, n.text AS note FROM source_records AS r JOIN source_notes AS n ON r.id = n.id", "SELECT r.`label` AS `label`, `r`.`id` AS `id`, n.text AS note FROM source_records AS r JOIN source_notes AS n ON r.id = n.id"},
			{"joined aliases", "SELECT n.text AS note, r.id AS id FROM source_records AS r JOIN source_notes AS n ON r.id = n.id", "SELECT n.text AS note, r.id AS id FROM source_records AS r JOIN source_notes AS n ON r.id = n.id"},
		} {
			t.Run(verb+"/"+tt.name, func(t *testing.T) {
				prefix := "-- name: Write :many\n" + verb + " INTO records "
				result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: prefix + tt.selectSQL + " RETURNING *;"}})
				if err != nil {
					t.Fatal(err)
				}
				query := result.Queries[0]
				wantSQL := prefix + tt.expandedSQL + " RETURNING `id`, `label`, `note`;"
				if query.SQL != wantSQL {
					t.Fatalf("SQL = %q, want %q", query.SQL, wantSQL)
				}
				if len(query.ResultSets) != 1 || !reflect.DeepEqual(query.ResultSets[0].Columns, result.Catalog.Tables[0].Columns) {
					t.Fatalf("RETURNING must use destination types and column order: %#v", query.ResultSets)
				}
			})
		}
	}
}

func TestNamedInsertSelectInfersFilterParameters(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));"}}
	sql := "-- name: Copy :exec\nINSERT INTO records SELECT * FROM records AS r WHERE r.id >= $minimum LIMIT $count;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Parameter{{Name: "minimum", Type: model.Type{Kind: "Uint64"}}, {Name: "count", Type: model.Type{Kind: "Uint64"}}}
	if !reflect.DeepEqual(result.Queries[0].Parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", result.Queries[0].Parameters, want)
	}
}

func TestNamedInsertSelectDiagnostics(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));"}}
	for _, tt := range []struct{ name, sql, want string }{
		{"missing key", "SELECT label FROM records", `missing primary key column "id"`},
		{"unknown target", "SELECT id, label AS extra FROM records", `unknown target column "extra"`},
		{"duplicate", "SELECT id, id FROM records", `duplicate source column "id"`},
		{"duplicate wildcard", "SELECT *, id FROM records", `duplicate source column "id"`},
		{"duplicate alias", "SELECT id AS id, label AS `id` FROM records", `duplicate source column "id"`},
		{"type mismatch", "SELECT label AS id FROM records", `source column "id" has type Optional<Utf8>`},
		{"null key", "SELECT NULL AS id", `source column "id" has type Null`},
		{"missing alias", "SELECT 1ul", `unknown target column "column0"`},
		{"bad source", "SELECT id FROM missing", `unknown table "missing"`},
		{"unknown qualifier", "SELECT absent.* FROM records", `unknown table or alias "absent"`},
		{"qualified join", "SELECT r.id FROM records r JOIN records other ON r.id = other.id", `unknown target column "r.id"; use AS id`},
		{"union", "SELECT id FROM records UNION ALL SELECT id FROM records", `INSERT/UPSERT SELECT supports one SELECT input`},
		{"except", "SELECT id FROM records EXCEPT SELECT id FROM records", `EXCEPT is not yet supported`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\nUPSERT INTO records " + tt.sql + ";"}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestNamedInsertSelectAllowsGeneratedKey(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Serial, tenant Uint64 NOT NULL, label Utf8, PRIMARY KEY(tenant, id));"}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		t.Run(verb, func(t *testing.T) {
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :many\n" + verb + " INTO records SELECT 1ul AS tenant, NULL AS label RETURNING id;"}})
			if err != nil {
				t.Fatal(err)
			}
			column := result.Queries[0].ResultSets[0].Columns[0]
			if column.Name != "id" || column.Type.Kind != "Int32" || !column.SequenceGenerated {
				t.Fatalf("generated key metadata = %#v", column)
			}
			_, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + verb + " INTO records SELECT NULL AS label;"}})
			if err == nil || !strings.Contains(err.Error(), `missing primary key column "tenant"`) {
				t.Fatalf("error = %v, want missing ordinary key diagnostic", err)
			}
		})
	}
	for _, statement := range []string{"UPDATE records", "DELETE FROM records"} {
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + statement + " ON SELECT 1ul AS tenant;"}})
		if err == nil || !strings.Contains(err.Error(), `missing primary key column "id"`) {
			t.Fatalf("%s error = %v, want missing generated key diagnostic", statement, err)
		}
	}
}

func TestNamedInsertSelectRequiresNotNullColumns(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8 NOT NULL, note Utf8, PRIMARY KEY(id));"}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		t.Run(verb, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + verb + " INTO records SELECT 1ul AS id;"}})
			if err == nil || !strings.Contains(err.Error(), `missing required column "label"`) {
				t.Fatalf("error = %v, want missing NOT NULL column diagnostic", err)
			}
			if _, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + verb + " INTO records SELECT 1ul AS id, 'hello'u AS label;"}}); err != nil {
				t.Fatalf("omitting nullable note: %v", err)
			}
		})
	}
	for _, statement := range []string{"UPDATE records", "DELETE FROM records"} {
		if _, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + statement + " ON SELECT 1ul AS id;"}}); err != nil {
			t.Fatalf("%s without unrelated NOT NULL column: %v", statement, err)
		}
	}
}

func TestTrailingStructComma(t *testing.T) {
	for _, typ := range []string{"Struct<id:Uint64,>", "List<Struct<id:Uint64,note:Utf8?,>>", "Struct<inner:Struct<id:Uint64,>,>"} {
		parsed, err := parseType(typ)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if strings.Contains(parsed.String(), ",>") {
			t.Fatalf("noncanonical type: %s", parsed.String())
		}
	}
	for _, typ := range []string{"Struct<,>", "Struct<id:Uint64,,>", "Struct<id:Uint64,id:Utf8,>"} {
		if _, err := parseType(typ); err == nil {
			t.Fatalf("accepted malformed type %s", typ)
		}
	}
}
