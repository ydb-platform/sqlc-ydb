package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func TestAnalyzeAppliesSchemaMigrationsAcrossSources(t *testing.T) {
	schema := []model.Source{
		{Name: "001_create.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8, PRIMARY KEY (id));`},
		{Name: "002_alter.sql", Text: `ALTER TABLE authors ADD COLUMN biography Utf8 NOT NULL, DROP COLUMN name;`},
	}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: ListAuthors :many
SELECT id, biography FROM authors;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	wantColumns := []model.Column{
		{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "authors"},
		{Name: "biography", Type: model.Type{Kind: "Utf8"}, Table: "authors"},
	}
	if !reflect.DeepEqual(got.Catalog.Tables[0].Columns, wantColumns) {
		t.Fatalf("catalog columns = %#v, want %#v", got.Catalog.Tables[0].Columns, wantColumns)
	}
	if !reflect.DeepEqual(got.Queries[0].ResultSets[0].Columns, wantColumns) {
		t.Fatalf("result columns = %#v, want %#v", got.Queries[0].ResultSets[0].Columns, wantColumns)
	}
}

func TestCatalogDropRecreateAndRenamePreserveOrder(t *testing.T) {
	catalog, diagnostics := buildCatalog([]model.Source{
		{Name: "001.sql", Text: `CREATE TABLE first (id Uint64 NOT NULL, PRIMARY KEY (id)); CREATE TABLE second (id Uint64 NOT NULL, PRIMARY KEY (id));`},
		{Name: "002.sql", Text: `DROP TABLE first; CREATE TABLE first (key Utf8 NOT NULL, PRIMARY KEY (key)); ALTER TABLE first RENAME TO final;`},
	})
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if got, want := []string{catalog.Tables[0].Name, catalog.Tables[1].Name}, []string{"second", "final"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("table order = %#v, want %#v", got, want)
	}
	if got := catalog.Tables[1].Columns[0]; got.Name != "key" || got.Table != "final" {
		t.Fatalf("renamed column = %#v", got)
	}
}

func TestCatalogExistenceGuards(t *testing.T) {
	catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: `
DROP TABLE IF EXISTS missing;
CREATE TABLE IF NOT EXISTS authors (id Uint64 NOT NULL, PRIMARY KEY (id));
CREATE TABLE IF NOT EXISTS authors (invalid_replacement Utf8);`}})
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if len(catalog.Tables) != 1 || catalog.Tables[0].Columns[0].Name != "id" {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestCatalogRenamesQuotedTablePathAndColumnOwnership(t *testing.T) {
	catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE ` + "`dir/authors`" + ` (id Uint64 NOT NULL, PRIMARY KEY (id));
ALTER TABLE ` + "`dir/authors`" + ` RENAME TO ` + "`archive/writers`" + `;`}})
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if catalog.Tables[0].Name != "archive/writers" || catalog.Tables[0].Columns[0].Table != "archive/writers" {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestCatalogReportsMigrationFailuresAtActionSource(t *testing.T) {
	_, diagnostics := buildCatalog([]model.Source{
		{Name: "001.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`},
		{Name: "002.sql", Text: "\nALTER TABLE authors ADD COLUMN id Utf8;"},
	})
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	got := diagnostics[0]
	if got.Position != (model.Position{File: "002.sql", Line: 2, Column: 32}) || !strings.Contains(got.Message, `column "id" already exists`) {
		t.Fatalf("diagnostic = %#v", got)
	}
}

func TestCatalogRejectsRenameCollisionWithoutMutation(t *testing.T) {
	catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));
CREATE TABLE writers (id Uint64 NOT NULL, PRIMARY KEY (id));
ALTER TABLE authors RENAME TO writers;`}})
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, `table "writers" already exists`) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if got := []string{catalog.Tables[0].Name, catalog.Tables[1].Name}; !reflect.DeepEqual(got, []string{"authors", "writers"}) {
		t.Fatalf("table names after failed rename = %#v", got)
	}
}

func TestCatalogRejectsRenameCombinedWithOtherActions(t *testing.T) {
	catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));
ALTER TABLE authors ADD COLUMN name Utf8, RENAME TO writers;`}})
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "RENAME TO must be the only action") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if catalog.Tables[0].Name != "authors" || len(catalog.Tables[0].Columns) != 1 {
		t.Fatalf("catalog after rejected mixed rename = %#v", catalog)
	}
}

func TestCatalogRejectsExplainedSchemaStatementWithoutMutation(t *testing.T) {
	catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: `EXPLAIN CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}})
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "unsupported schema statement") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if len(catalog.Tables) != 0 {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestCatalogGuardDoesNotHideUnsupportedCreateForm(t *testing.T) {
	_, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));
CREATE TABLE IF NOT EXISTS authors (PRIMARY KEY (id)) AS SELECT 1 AS id;`}})
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "only CREATE TABLE with an explicit column list is supported") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestCatalogDoesNotApplyDDLInsideActionDefinition(t *testing.T) {
	catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));
DEFINE ACTION $change_schema() AS
    ALTER TABLE authors ADD COLUMN biography Utf8;
END DEFINE;`}})
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "unsupported schema statement") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	want := []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "authors"}}
	if len(catalog.Tables) != 1 || !reflect.DeepEqual(catalog.Tables[0].Columns, want) {
		t.Fatalf("action definition changed catalog: %#v", catalog)
	}
}

func TestCatalogRejectsMissingObjectsAndPrimaryKeyChanges(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "drop missing table", sql: `DROP TABLE missing;`, want: `table "missing" does not exist`},
		{name: "alter missing table", sql: `ALTER TABLE missing ADD COLUMN value Utf8;`, want: `table "missing" does not exist`},
		{name: "drop missing column", sql: `CREATE TABLE t (id Uint64 NOT NULL, PRIMARY KEY (id)); ALTER TABLE t DROP COLUMN missing;`, want: `column "missing" does not exist`},
		{name: "drop key column", sql: `CREATE TABLE t (id Uint64 NOT NULL, PRIMARY KEY (id)); ALTER TABLE t DROP COLUMN id;`, want: `cannot drop primary key column "id"`},
		{name: "duplicate key column", sql: `CREATE TABLE t (id Uint64 NOT NULL, PRIMARY KEY (id, id));`, want: `primary key column "id" is declared more than once`},
		{name: "missing primary key", sql: `CREATE TABLE t (id Uint64 NOT NULL);`, want: `must declare a PRIMARY KEY`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: tt.sql}})
			if len(diagnostics) == 0 || !strings.Contains(diagnostics[0].Message, tt.want) {
				t.Fatalf("diagnostics = %#v, want message containing %q", diagnostics, tt.want)
			}
		})
	}
}

func TestCatalogRejectsUnsupportedSchemaOperations(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "alter nullability", sql: `CREATE TABLE t (id Uint64 NOT NULL, value Utf8, PRIMARY KEY (id)); ALTER TABLE t ALTER COLUMN value SET NOT NULL;`, want: "unsupported ALTER TABLE action"},
		{name: "index", sql: `CREATE TABLE t (id Uint64 NOT NULL, PRIMARY KEY (id)); ALTER TABLE t ADD INDEX by_id GLOBAL ON (id);`, want: "unsupported ALTER TABLE action"},
		{name: "data statement", sql: `CREATE TABLE t (id Uint64 NOT NULL, PRIMARY KEY (id)); UPSERT INTO t (id) VALUES (1);`, want: "unsupported schema statement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: tt.sql}})
			if len(diagnostics) == 0 || !strings.Contains(diagnostics[0].Message, tt.want) {
				t.Fatalf("diagnostics = %#v, want message containing %q", diagnostics, tt.want)
			}
		})
	}
}

func TestCatalogDoesNotPartiallyApplyFailedMultiActionAlter(t *testing.T) {
	catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));
ALTER TABLE authors ADD COLUMN biography Utf8, DROP COLUMN missing;`}})
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, `column "missing" does not exist`) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	want := []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "authors"}}
	if !reflect.DeepEqual(catalog.Tables[0].Columns, want) {
		t.Fatalf("columns after failed ALTER = %#v, want %#v", catalog.Tables[0].Columns, want)
	}
}
