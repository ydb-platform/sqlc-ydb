package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const indexedRecordsSchema = `CREATE TABLE records (
    id Uint64 NOT NULL,
    label Utf8,
    note Utf8,
    PRIMARY KEY(id),
    INDEX by_label GLOBAL SYNC ON (label) COVER (note)
);`

func TestAnalyzeSecondaryIndexView(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: indexedRecordsSchema}}
	sql := "-- name: Read :many\nSELECT r.* FROM records VIEW by_label AS r WHERE r.label = $label;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	query := result.Queries[0]
	if !strings.Contains(query.SQL, "FROM records VIEW by_label AS r") || strings.Contains(query.SQL, "r.*") {
		t.Fatalf("index selection or wildcard projection changed: %s", query.SQL)
	}
	if len(query.Parameters) != 1 || query.Parameters[0].Type.String() != "Optional<Utf8>" || len(query.ResultSets[0].Columns) != 3 {
		t.Fatalf("indexed query metadata = %#v", query)
	}
}

func TestCatalogSecondaryIndexDefinitions(t *testing.T) {
	for _, kind := range []string{"GLOBAL", "GLOBAL SYNC", "GLOBAL ASYNC"} {
		t.Run(kind, func(t *testing.T) {
			sql := "CREATE TABLE records (INDEX `by label` " + kind + " ON (note, label) COVER (extra), id Uint64 NOT NULL, label Utf8, note Utf8, extra Utf8, PRIMARY KEY(id));"
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sql}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantKind := "GlobalSync"
			if kind == "GLOBAL ASYNC" {
				wantKind = "GlobalAsync"
			}
			want := []model.Index{{Name: "by label", Kind: wantKind, Columns: []string{"note", "label"}, DataColumns: []string{"extra"}}}
			if !reflect.DeepEqual(result.Catalog.Tables[0].Indexes, want) {
				t.Fatalf("indexes = %#v, want %#v", result.Catalog.Tables[0].Indexes, want)
			}
		})
	}
}

func TestCatalogSecondaryIndexMigrations(t *testing.T) {
	sql := indexedRecordsSchema + `
ALTER TABLE records ADD INDEX by_note GLOBAL ASYNC ON (note);
ALTER TABLE records DROP INDEX by_label;
ALTER TABLE records RENAME TO archive;`
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sql}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT * FROM archive VIEW by_note;"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Index{{Name: "by_note", Kind: "GlobalAsync", Columns: []string{"note"}}}
	if !reflect.DeepEqual(result.Catalog.Tables[0].Indexes, want) {
		t.Fatalf("indexes = %#v, want %#v", result.Catalog.Tables[0].Indexes, want)
	}
	if result.Queries[0].ResultSets[0].Columns[0].Table != "archive" {
		t.Fatalf("index read lost renamed base table: %#v", result.Queries[0].ResultSets)
	}
}

func TestCatalogSecondaryIndexDiagnostics(t *testing.T) {
	base := "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, note Utf8, PRIMARY KEY(id), %s);"
	for _, tt := range []struct{ name, definition, want string }{
		{"duplicate name", "INDEX by_label GLOBAL ON (label), INDEX `by_label` GLOBAL ON (note)", `index "by_label" is declared more than once`},
		{"unknown key", "INDEX by_label GLOBAL ON (missing)", `references unknown key column "missing"`},
		{"unknown cover", "INDEX by_label GLOBAL ON (label) COVER (missing)", `references unknown covering column "missing"`},
		{"duplicate key", "INDEX by_label GLOBAL ON (label, label)", `repeats key column "label"`},
		{"duplicate cover", "INDEX by_label GLOBAL ON (label) COVER (note, note)", `repeats covering column "note"`},
		{"cover index key", "INDEX by_label GLOBAL ON (label) COVER (label)", `covering column "label" is already part of the index key`},
		{"cover primary key", "INDEX by_label GLOBAL ON (label) COVER (id)", `covering column "id" is already part of the index key`},
		{"empty name", "INDEX `` GLOBAL ON (label)", `index name must not be empty`},
		{"local", "INDEX by_label LOCAL ON (label)", `unsupported index type`},
		{"unique", "INDEX by_label GLOBAL UNIQUE ON (label)", `unsupported index type`},
		{"using", "INDEX by_label GLOBAL USING vector_kmeans_tree ON (label)", `unsupported index type`},
		{"settings", "INDEX by_label GLOBAL ON (label) WITH (UNIFORM_PARTITIONS = 2)", `index settings are not yet supported`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze([]model.Source{{Name: "schema.sql", Text: strings.Replace(base, "%s", tt.definition, 1)}}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
	for _, tt := range []struct{ statement, want string }{
		{"ADD INDEX by_label GLOBAL ON (note)", `index "by_label" already exists`},
		{"ADD INDEX by_missing GLOBAL ON (missing)", `references unknown key column "missing"`},
		{"ADD INDEX by_note LOCAL ON (note)", `unsupported index type`},
		{"DROP INDEX missing", `index "missing" does not exist`},
		{"DROP COLUMN label", `cannot drop column "label" used by index "by_label"`},
		{"DROP COLUMN note", `cannot drop column "note" used by index "by_label"`},
	} {
		t.Run(tt.statement, func(t *testing.T) {
			_, err := Analyze([]model.Source{{Name: "schema.sql", Text: indexedRecordsSchema + " ALTER TABLE records " + tt.statement + ";"}}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCatalogIndexAlterIsAtomic(t *testing.T) {
	base, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: indexedRecordsSchema}})
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for _, changes := range []string{"DROP INDEX by_label, DROP INDEX missing", "ADD INDEX by_note GLOBAL ON (note), DROP INDEX missing"} {
		catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: indexedRecordsSchema + " ALTER TABLE records " + changes + ";"}})
		if len(diagnostics) == 0 || !reflect.DeepEqual(catalog, base) {
			t.Fatalf("%s: catalog = %#v, diagnostics = %v", changes, catalog, diagnostics)
		}
	}
}

func TestCatalogIndexAndColumnMigrationOrder(t *testing.T) {
	sql := indexedRecordsSchema + `
ALTER TABLE records DROP INDEX by_label, DROP COLUMN note, ADD COLUMN extra Utf8, ADD INDEX by_extra GLOBAL ON (extra);`
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sql}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	table := result.Catalog.Tables[0]
	if tableColumn(&table, "note") != nil || tableColumn(&table, "extra") == nil || len(table.Indexes) != 1 || table.Indexes[0].Name != "by_extra" {
		t.Fatalf("index/column migration result = %#v", table)
	}
}

func TestAnalyzeIndexViewPreservesQualifiedSQL(t *testing.T) {
	schemaSQL := strings.ReplaceAll(indexedRecordsSchema, "records", "`records/items`")
	schemaSQL = strings.ReplaceAll(schemaSQL, "by_label", "`by label`")
	sql := "-- name: Read :many\nSELECT r.* FROM `records/items` /* table */ VIEW /* index */ `by label` AS r WHERE r.label = $label;"
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: schemaSQL}}, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sql, "r.*", "r.`id` AS `id`, `r`.`label` AS `label`, `r`.`note` AS `note`", 1)
	query := result.Queries[0]
	if query.SQL != want || !reflect.DeepEqual(query.Syntax.Relations, []model.TableBinding{{Table: "records/items", Alias: "r"}}) {
		t.Fatalf("SQL = %q, relations = %#v", query.SQL, query.Syntax.Relations)
	}
}

func TestAnalyzeIndexViewsInJoinsAndDML(t *testing.T) {
	for _, tt := range []struct{ command, sql string }{
		{":many", "SELECT r.* FROM records VIEW by_label AS r JOIN records AS other ON r.id = other.id"},
		{":exec", "INSERT INTO records SELECT * FROM records VIEW by_label WHERE label = $label"},
		{":exec", "UPSERT INTO records (label, id) SELECT label, id FROM records VIEW by_label"},
		{":exec", "UPDATE records ON SELECT id, note FROM records VIEW by_label"},
		{":exec", "DELETE FROM records ON SELECT id FROM records VIEW by_label"},
	} {
		t.Run(tt.sql, func(t *testing.T) {
			sql := "-- name: Read " + tt.command + "\n" + tt.sql + ";"
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: indexedRecordsSchema}}, []model.Source{{Name: "query.sql", Text: sql}})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Queries[0].SQL, "records VIEW by_label") {
				t.Fatalf("index selection lost: %s", result.Queries[0].SQL)
			}
		})
	}
}

func TestAnalyzeIndexViewKeepsBaseTableColumns(t *testing.T) {
	schema := indexedRecordsSchema + " ALTER TABLE records ADD COLUMN payload String;"
	query := "-- name: Read :many\nSELECT payload FROM records VIEW by_label;"
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: query}})
	if err != nil {
		t.Fatal(err)
	}
	column := result.Queries[0].ResultSets[0].Columns[0]
	if column.Name != "payload" || column.Type.String() != "Optional<String>" || column.Table != "records" {
		t.Fatalf("non-covering result = %#v", column)
	}
}

func TestAnalyzeIndexViewDiagnostics(t *testing.T) {
	for _, tt := range []struct{ sql, want string }{
		{"SELECT id FROM missing VIEW by_label", `unknown table "missing"`},
		{"SELECT id FROM records VIEW PRIMARY KEY", "VIEW PRIMARY KEY is not yet supported"},
		{"SELECT missing FROM records VIEW by_label", `unknown column "missing"`},
		{"SELECT id FROM records VIEW by_missing", `unknown index "by_missing" on table "records"`},
		{"SELECT id FROM other.records VIEW by_label", "cluster-qualified and temporary table references are unsupported"},
		{"SELECT id FROM @records VIEW by_label", "cluster-qualified and temporary table references are unsupported"},
	} {
		t.Run(tt.sql, func(t *testing.T) {
			_, err := Analyze([]model.Source{{Name: "schema.sql", Text: indexedRecordsSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tt.sql + ";"}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAnalyzeSecondaryIndexViewWithoutMetadata(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT id FROM records VIEW missing;"}})
	if err == nil || !strings.Contains(err.Error(), `unknown index "missing" on table "records"`) {
		t.Fatalf("error = %v, want index diagnostic on the underlying table", err)
	}
}
