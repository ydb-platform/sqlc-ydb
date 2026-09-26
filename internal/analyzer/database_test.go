package analyzer

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
)

type fakeAnalysisDatabase struct {
	tables        map[string]model.Table
	described     []string
	validated     []string
	describeError error
	validateError error
}

func (d *fakeAnalysisDatabase) DescribeTable(_ context.Context, name string) (model.Table, error) {
	d.described = append(d.described, name)
	if d.describeError != nil {
		return model.Table{}, d.describeError
	}
	table, ok := d.tables[name]
	if !ok {
		return model.Table{}, errors.New("table does not exist")
	}
	table.Columns = slices.Clone(table.Columns)
	return table, nil
}

func (d *fakeAnalysisDatabase) ValidateQuery(_ context.Context, sql string) error {
	d.validated = append(d.validated, sql)
	return d.validateError
}

func databaseTestTable(textColumn string) model.Table {
	return model.Table{Columns: []model.Column{
		{Name: "id", Type: model.Type{Kind: "Uint64"}},
		{Name: textColumn, Type: model.Type{Kind: "Utf8"}},
	}, PrimaryKey: []string{"id"}}
}

func TestDatabaseAnalysisSendsOriginalSQLBeforeLocalResolution(t *testing.T) {
	for _, sql := range []string{
		"-- name: Read :one\r\nSELECT id + $id AS next FROM records;",
		"-- name: Read :one\nWITH r AS (SELECT id FROM records) SELECT id FROM r WHERE id = $id;",
		"-- name: Read :one\nSELECT Custom::Unknown($id) AS value FROM records;",
		"-- name: Read :one\nSELECT id FROM records WHERE id = $id;",
	} {
		t.Run(sql, func(t *testing.T) {
			database := &fakeAnalysisDatabase{validateError: errors.New("Unknown name: $id; add DECLARE with the parameter type")}
			_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
			require.Error(t, err)
			require.Contains(t, err.Error(), "database query validation failed: Unknown name:")
			require.Contains(t, err.Error(), "add DECLARE")
			require.Equal(t, []string{sql}, database.validated)
			require.Len(t, database.described, 0)
		})
	}
}

func TestDatabaseAnalysisKeepsAnnotationChecksBeforeServerCalls(t *testing.T) {
	for _, sql := range []string{
		"-- name: Duplicate :one\nSELECT id FROM records;\n-- name: Duplicate :one\nSELECT id FROM records;",
		"-- name: Bad :unknown\nSELECT id FROM records;",
		"SELECT id FROM records;",
	} {
		t.Run(sql, func(t *testing.T) {
			database := &fakeAnalysisDatabase{}
			_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
			require.Error(t, err)
			require.Len(t, database.described, 0)
			require.Len(t, database.validated, 0)
		})
	}
}

func TestOfflineAnalysisStillInfersUndeclaredParameters(t *testing.T) {
	sql := "-- name: Read :one\nSELECT id FROM records WHERE id = $id;"
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	query := result.Queries[0]
	require.Equal(t, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, query.Parameters)
	require.Len(t, query.DeclaredParameters, 0)
	require.Equal(t, sql, query.SQL)
}

func TestDatabaseAnalysisDiscoversReferencedTables(t *testing.T) {
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{
		"authors": databaseTestTable("name"), "books": databaseTestTable("title"), "archive/writers": databaseTestTable("name"),
	}}
	queries := []model.Source{{Name: "queries.sql", Text: `-- name: List :many
-- FROM comment_table
$text = "FROM string_table"u;
SELECT a.id AS author_id, b.title AS title
FROM authors AS a JOIN books AS b ON a.id = b.id;
-- name: Copy :exec
UPSERT INTO books (id, title) SELECT id, name FROM authors;
-- name: Rename :exec
DECLARE $name AS Utf8;
DECLARE $id AS Uint64;
UPDATE authors SET name = $name WHERE id = $id;
-- name: Delete :exec
DECLARE $id AS Uint64;
DELETE FROM books WHERE id = $id;
-- name: Archive :one
DECLARE $id AS Uint64;
SELECT id, name FROM ` + "`archive/writers`" + ` WHERE id = $id;
-- name: Batch :exec
DECLARE $rows AS List<Struct<id:Uint64,name:Utf8>>;
UPSERT INTO authors (id, name) SELECT id, name FROM AS_TABLE($rows);`}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, queries, Options{}, database)
	require.NoError(t, err)
	{
		want := []string{"authors", "books", "archive/writers"}
		require.Equal(t, want, database.described)
	}
	require.Len(t, result.Queries, 6)
	require.Len(t, database.validated, 6)
	{
		got := result.Queries[2].Parameters
		require.Equal(t, []model.Parameter{{Name: "name", Type: model.Type{Kind: "Utf8"}}, {Name: "id", Type: model.Type{Kind: "Uint64"}}}, got)
	}
	for _, table := range result.Catalog.Tables {
		for _, column := range table.Columns {
			require.Equal(t, table.Name, column.Table)
		}
	}
}

func TestDatabaseAnalysisPreservesSQLAndDeclarations(t *testing.T) {
	sql := "-- name: Read :one\r\n-- DECLARE $fake AS Utf8;\r\nDECLARE $id AS Uint64;\r\nDECLARE $`имя` AS Utf8;\r\n$local = $id;\r\nSELECT name FROM authors WHERE id = $local AND name = $`имя`;"
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"authors": databaseTestTable("name")}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
	require.NoError(t, err)
	query := result.Queries[0]
	require.Equal(t, sql, query.SQL)
	require.Equal(t, []string{"id", "имя"}, query.DeclaredParameters)
	require.Len(t, query.Parameters, 2)
	require.Equal(t, "имя", query.Parameters[1].Name)
	{
		want := sql
		require.Len(t, database.validated, 1)
		require.Equal(t, want, database.validated[0])
	}
}

func TestDatabaseAnalysisPreservesFunctionContracts(t *testing.T) {
	database := &fakeAnalysisDatabase{}
	options := Options{Functions: []builtins.Signature{{Name: "IsReady", Returns: model.Type{Kind: "Bool"}}}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: "-- name: Ready :one\nSELECT IsReady() AS ready;"}}, options, database)
	require.NoError(t, err)
	{
		got := result.Queries[0].ResultSets[0].Columns[0].Type
		require.Equal(t, "Bool", got.Kind)
	}
}

func TestDatabaseAnalysisFixesWildcardWireOrderInSQL(t *testing.T) {
	for _, localSchema := range []bool{false, true} {
		t.Run(map[bool]string{false: "discovered", true: "local"}[localSchema], func(t *testing.T) {
			var schema []model.Source
			if localSchema {
				schema = []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (z Utf8 NOT NULL, a Utf8 NOT NULL, id Uint64 NOT NULL, PRIMARY KEY(id));"}}
			}
			database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": {
				Columns: []model.Column{{Name: "z", Type: model.Type{Kind: "Utf8"}}, {Name: "a", Type: model.Type{Kind: "Utf8"}}, {Name: "id", Type: model.Type{Kind: "Uint64"}}}, PrimaryKey: []string{"id"},
			}}}
			result, err := AnalyzeWithDatabase(context.Background(), schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT * FROM records;"}}, Options{}, database)
			require.NoError(t, err)
			var names []string
			for _, column := range result.Queries[0].ResultSets[0].Columns {
				names = append(names, column.Name)
			}
			require.Equal(t, []string{"z", "a", "id"}, names)
			{
				sql := result.Queries[0].SQL
				require.Equal(t, "-- name: Read :many\nSELECT `z`, `a`, `id` FROM records;", sql)
			}
		})
	}
}

func TestDatabaseAnalysisRejectsSchemaDrift(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id));"}}
	queries := []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT id, name FROM records;"}}
	tests := []struct {
		name   string
		change func(*model.Table)
		want   string
	}{
		{"type", func(table *model.Table) { table.Columns[1].Type = model.Type{Kind: "String"} }, "local type Utf8 and database type String"},
		{"nullability", func(table *model.Table) { table.Columns[1].Type = model.Optional(table.Columns[1].Type) }, "database type Optional<Utf8>"},
		{"missing column", func(table *model.Table) { table.Columns = table.Columns[:1] }, "column \"name\" is missing from the database"},
		{"extra column", func(table *model.Table) {
			table.Columns = append(table.Columns, model.Column{Name: "extra", Type: model.Type{Kind: "Utf8"}})
		}, "database column \"extra\" is missing from the local schema"},
		{"primary key", func(table *model.Table) { table.PrimaryKey = []string{"name", "id"} }, "local primary key [id] differs"},
		{"sequence", func(table *model.Table) { table.Columns[0].SequenceGenerated = true }, "different sequence generation settings"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			table := databaseTestTable("name")
			test.change(&table)
			database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": table}}
			result, err := AnalyzeWithDatabase(context.Background(), schema, queries, Options{}, database)
			require.Error(t, err)
			require.Contains(t, err.Error(), test.want)
			require.Contains(t, err.Error(), "query.sql:2:")
			require.Equal(t, []string{queries[0].Text}, database.validated)
			require.Len(t, result.Queries, 0)
		})
	}
}

func TestDatabaseAnalysisSchemaDriftUsesFirstTableReference(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id));"}}
	queries := []model.Source{
		{Name: "z-first.sql", Text: `-- name: Prelude :one
SELECT 1 AS value;

-- name: FirstReference :many
SELECT r.id AS id
FROM records AS r
JOIN records AS again ON r.id = again.id;
-- name: LaterSameFile :many
SELECT id FROM records;`},
		{Name: "a-later.sql", Text: "-- name: LaterFile :many\nSELECT id FROM records;"},
	}
	table := databaseTestTable("name")
	table.Columns[1].Type = model.Type{Kind: "String"}
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": table}}
	result, err := AnalyzeWithDatabase(context.Background(), schema, queries, Options{}, database)
	require.Error(t, err)
	require.Len(t, result.Diagnostics, 1)
	diagnostic := result.Diagnostics[0]
	{
		want := (model.Position{File: "z-first.sql", Line: 6, Column: 6})
		require.Equal(t, want, diagnostic.Position)
	}
	require.Contains(t, diagnostic.Message, `database schema drift for table "records"`)
	require.Contains(t, diagnostic.Message, "local type Utf8 and database type String")
}

func TestDatabaseAnalysisChecksPrimaryKeyOrder(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id, name));"}}
	table := databaseTestTable("name")
	table.PrimaryKey = []string{"name", "id"}
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": table}}
	_, err := AnalyzeWithDatabase(context.Background(), schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT id FROM records;"}}, Options{}, database)
	require.ErrorContains(t, err, "local primary key [id name] differs from database primary key [name id]")
	require.Len(t, database.validated, 1)
}

func TestDatabaseAnalysisRejectsUnsupportedSourcesBeforeDiscovery(t *testing.T) {
	for _, sql := range []string{
		"-- name: Read :one\nWITH r AS (SELECT id FROM records) SELECT id FROM r;",
		"-- name: Read :one\nSELECT id FROM $table;",
		"-- name: Read :one\nSELECT id FROM cluster.records;",
		"-- name: Bad :exec\nCREATE TABLE records (id Uint64, PRIMARY KEY(id));",
		"-- name: Bad :one\nSELECT id FROM records; SELECT id FROM records;",
		"-- name: Bad :one\nSELECT sqlc.slice(ids) FROM records;",
		"-- name: Bad :one\nDECLARE $id AS Mystery; SELECT id FROM records;",
	} {
		t.Run(sql, func(t *testing.T) {
			database := &fakeAnalysisDatabase{}
			_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
			require.Error(t, err)
			require.Len(t, database.described, 0)
			require.Equal(t, []string{sql}, database.validated)
		})
	}
}

func TestDatabaseAnalysisRejectsValuesSource(t *testing.T) {
	const sql = "-- name: Read :one\nSELECT * FROM (VALUES (1u));"
	database := &fakeAnalysisDatabase{}
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported FROM or JOIN source")
	require.Len(t, database.described, 0)
	require.Equal(t, []string{sql}, database.validated)
}

func TestDatabaseAnalysisPropagatesErrors(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT id FROM records;"}}
	for _, test := range []struct {
		name     string
		database fakeAnalysisDatabase
		want     string
	}{
		{"missing table", fakeAnalysisDatabase{}, "table does not exist"},
		{"connection", fakeAnalysisDatabase{describeError: errors.New("connection refused")}, "connection refused"},
		{"unknown type", fakeAnalysisDatabase{tables: map[string]model.Table{"records": {Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Mystery"}}}}}}, "unsupported YQL type Mystery"},
		{"duplicate database columns", fakeAnalysisDatabase{tables: map[string]model.Table{"records": databaseTestTable("id")}}, `database returned an empty or duplicate column name "id"`},
		{"server compilation", fakeAnalysisDatabase{tables: map[string]model.Table{"records": databaseTestTable("name")}, validateError: errors.New("server compilation failed")}, "database query validation failed: server compilation failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := AnalyzeWithDatabase(context.Background(), nil, queries, Options{}, &test.database)
			require.Error(t, err)
			require.Contains(t, err.Error(), test.want)
			require.Contains(t, err.Error(), "query.sql:")
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	database := &fakeAnalysisDatabase{}
	_, err := AnalyzeWithDatabase(ctx, nil, queries, Options{}, database)
	require.Error(t, err)
	require.Contains(t, err.Error(), "context canceled")
	require.Len(t, database.described, 0)
	require.Len(t, database.validated, 0)
}

func TestDatabaseAnalysisKeepsOfflineExpressionLimits(t *testing.T) {
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": databaseTestTable("name")}}
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT id / 1 AS next FROM records;"}}, Options{}, database)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported arithmetic operator")
	require.Len(t, database.validated, 1)
}
