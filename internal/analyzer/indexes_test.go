package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
	require.NoError(t, err)
	query := result.Queries[0]
	require.Contains(t, query.SQL, "FROM records VIEW by_label AS r")
	require.NotContains(t, query.SQL, "r.*")
	require.Len(t, query.Parameters, 1)
	require.Equal(t, "Optional<Utf8>", query.Parameters[0].Type.String())
	require.Len(t, query.ResultSets[0].Columns, 3)
}

func TestCatalogSecondaryIndexDefinitions(t *testing.T) {
	for _, kind := range []string{"GLOBAL", "GLOBAL SYNC", "GLOBAL ASYNC"} {
		t.Run(kind, func(t *testing.T) {
			sql := "CREATE TABLE records (INDEX `by label` " + kind + " ON (note, label) COVER (extra), id Uint64 NOT NULL, label Utf8, note Utf8, extra Utf8, PRIMARY KEY(id));"
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sql}}, nil)
			require.NoError(t, err)
			wantKind := "GlobalSync"
			if kind == "GLOBAL ASYNC" {
				wantKind = "GlobalAsync"
			}
			want := []model.Index{{Name: "by label", Kind: wantKind, Columns: []string{"note", "label"}, DataColumns: []string{"extra"}}}
			require.Equal(t, want, result.Catalog.Tables[0].Indexes)
		})
	}
}

func TestCatalogSecondaryIndexMigrations(t *testing.T) {
	sql := indexedRecordsSchema + `
ALTER TABLE records ADD INDEX by_note GLOBAL ASYNC ON (note);
ALTER TABLE records DROP INDEX by_label;
ALTER TABLE records RENAME TO archive;`
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sql}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT * FROM archive VIEW by_note;"}})
	require.NoError(t, err)
	want := []model.Index{{Name: "by_note", Kind: "GlobalAsync", Columns: []string{"note"}}}
	require.Equal(t, want, result.Catalog.Tables[0].Indexes)
	require.Equal(t, "archive", result.Queries[0].ResultSets[0].Columns[0].Table)
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
			require.ErrorContains(t, err, tt.want)
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
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCatalogIndexAlterIsAtomic(t *testing.T) {
	base, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: indexedRecordsSchema}})
	require.Len(t, diagnostics, 0)
	for _, changes := range []string{"DROP INDEX by_label, DROP INDEX missing", "ADD INDEX by_note GLOBAL ON (note), DROP INDEX missing"} {
		catalog, diagnostics := buildCatalog([]model.Source{{Name: "schema.sql", Text: indexedRecordsSchema + " ALTER TABLE records " + changes + ";"}})
		require.NotEqual(t, 0, len(diagnostics))
		require.Equal(t, base, catalog)
	}
}

func TestCatalogIndexAndColumnMigrationOrder(t *testing.T) {
	sql := indexedRecordsSchema + `
ALTER TABLE records DROP INDEX by_label, DROP COLUMN note, ADD COLUMN extra Utf8, ADD INDEX by_extra GLOBAL ON (extra);`
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sql}}, nil)
	require.NoError(t, err)
	table := result.Catalog.Tables[0]
	require.Nil(t, tableColumn(&table, "note"))
	require.NotNil(t, tableColumn(&table, "extra"))
	require.Len(t, table.Indexes, 1)
	require.Equal(t, "by_extra", table.Indexes[0].Name)
}

func TestAnalyzeIndexViewPreservesQualifiedSQL(t *testing.T) {
	schemaSQL := strings.ReplaceAll(indexedRecordsSchema, "records", "`records/items`")
	schemaSQL = strings.ReplaceAll(schemaSQL, "by_label", "`by label`")
	sql := "-- name: Read :many\nSELECT r.* FROM `records/items` /* table */ VIEW /* index */ `by label` AS r WHERE r.label = $label;"
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: schemaSQL}}, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	want := strings.Replace(sql, "r.*", "r.`id` AS `id`, `r`.`label` AS `label`, `r`.`note` AS `note`", 1)
	query := result.Queries[0]
	require.Equal(t, want, query.SQL)
	require.Equal(t, []model.TableBinding{{Table: "records/items", Alias: "r"}}, query.Syntax.Relations)
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
			require.NoError(t, err)
			require.Contains(t, result.Queries[0].SQL, "records VIEW by_label")
		})
	}
}

func TestAnalyzeIndexViewKeepsBaseTableColumns(t *testing.T) {
	schema := indexedRecordsSchema + " ALTER TABLE records ADD COLUMN payload String;"
	query := "-- name: Read :many\nSELECT payload FROM records VIEW by_label;"
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	column := result.Queries[0].ResultSets[0].Columns[0]
	require.Equal(t, "payload", column.Name)
	require.Equal(t, "Optional<String>", column.Type.String())
	require.Equal(t, "records", column.Table)
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
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestAnalyzeSecondaryIndexViewWithoutMetadata(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT id FROM records VIEW missing;"}})
	require.ErrorContains(t, err, `unknown index "missing" on table "records"`)
}
