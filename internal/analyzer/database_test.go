package analyzer

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

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
			if err == nil || !strings.Contains(err.Error(), "database query validation failed: Unknown name:") || !strings.Contains(err.Error(), "add DECLARE") {
				t.Fatalf("server diagnostic was replaced by local analysis: %v", err)
			}
			if !reflect.DeepEqual(database.validated, []string{sql}) {
				t.Fatalf("server SQL = %q, want original SQL once %q", database.validated, sql)
			}
			if len(database.described) != 0 {
				t.Fatalf("server rejection did not precede discovery: %v", database.described)
			}
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
			if err == nil || len(database.described) != 0 || len(database.validated) != 0 {
				t.Fatalf("invalid annotation reached database: error=%v describes=%v validations=%v", err, database.described, database.validated)
			}
		})
	}
}

func TestOfflineAnalysisStillInfersUndeclaredParameters(t *testing.T) {
	sql := "-- name: Read :one\nSELECT id FROM records WHERE id = $id;"
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	query := result.Queries[0]
	if !reflect.DeepEqual(query.Parameters, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}) || len(query.DeclaredParameters) != 0 || query.SQL != sql {
		t.Fatalf("offline inferred parameter contract changed: %#v", query)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"authors", "books", "archive/writers"}; !reflect.DeepEqual(database.described, want) {
		t.Fatalf("described %v, want %v", database.described, want)
	}
	if len(result.Queries) != 6 || len(database.validated) != 6 {
		t.Fatalf("queries=%d validation calls=%d", len(result.Queries), len(database.validated))
	}
	if got := result.Queries[2].Parameters; !reflect.DeepEqual(got, []model.Parameter{{Name: "name", Type: model.Type{Kind: "Utf8"}}, {Name: "id", Type: model.Type{Kind: "Uint64"}}}) {
		t.Fatalf("declared UPDATE parameters: %#v", got)
	}
	for _, table := range result.Catalog.Tables {
		for _, column := range table.Columns {
			if column.Table != table.Name {
				t.Fatalf("column lost logical owner: %#v", column)
			}
		}
	}
}

func TestDatabaseAnalysisPreservesSQLAndDeclarations(t *testing.T) {
	sql := "-- name: Read :one\r\n-- DECLARE $fake AS Utf8;\r\nDECLARE $id AS Uint64;\r\nDECLARE $`имя` AS Utf8;\r\n$local = $id;\r\nSELECT name FROM authors WHERE id = $local AND name = $`имя`;"
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"authors": databaseTestTable("name")}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
	if err != nil {
		t.Fatal(err)
	}
	query := result.Queries[0]
	if query.SQL != sql || !reflect.DeepEqual(query.DeclaredParameters, []string{"id", "имя"}) {
		t.Fatalf("source changed: %#v", query)
	}
	if len(query.Parameters) != 2 || query.Parameters[1].Name != "имя" {
		t.Fatalf("parameters include locals or lose names: %#v", query.Parameters)
	}
	if want := sql; len(database.validated) != 1 || database.validated[0] != want {
		t.Fatalf("validation SQL = %q, want %q", database.validated, want)
	}
}

func TestDatabaseAnalysisPreservesFunctionContracts(t *testing.T) {
	database := &fakeAnalysisDatabase{}
	options := Options{Functions: []builtins.Signature{{Name: "IsReady", Returns: model.Type{Kind: "Bool"}}}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: "-- name: Ready :one\nSELECT IsReady() AS ready;"}}, options, database)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Queries[0].ResultSets[0].Columns[0].Type; got.Kind != "Bool" {
		t.Fatalf("configured function type = %#v", got)
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
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, column := range result.Queries[0].ResultSets[0].Columns {
				names = append(names, column.Name)
			}
			if !reflect.DeepEqual(names, []string{"z", "a", "id"}) {
				t.Fatalf("wildcard column order = %v", names)
			}
			if sql := result.Queries[0].SQL; sql != "-- name: Read :many\nSELECT `z`, `a`, `id` FROM records;" {
				t.Fatalf("wildcard SQL does not fix the wire order: %q", sql)
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
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "query.sql:2:") {
				t.Fatalf("drift diagnostic = %v, want %q", err, test.want)
			}
			if !reflect.DeepEqual(database.validated, []string{queries[0].Text}) || len(result.Queries) != 0 {
				t.Fatal("schema drift did not stop analysis after server query validation")
			}
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
	if err == nil || len(result.Diagnostics) != 1 {
		t.Fatalf("schema drift diagnostics = %v, error = %v", result.Diagnostics, err)
	}
	diagnostic := result.Diagnostics[0]
	if want := (model.Position{File: "z-first.sql", Line: 6, Column: 6}); diagnostic.Position != want {
		t.Fatalf("schema drift position = %#v, want first table reference %#v", diagnostic.Position, want)
	}
	if !strings.Contains(diagnostic.Message, `database schema drift for table "records"`) || !strings.Contains(diagnostic.Message, "local type Utf8 and database type String") {
		t.Fatalf("unexpected diagnostic at first table reference: %s", diagnostic.Message)
	}
}

func TestDatabaseAnalysisChecksPrimaryKeyOrder(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id, name));"}}
	table := databaseTestTable("name")
	table.PrimaryKey = []string{"name", "id"}
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": table}}
	_, err := AnalyzeWithDatabase(context.Background(), schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT id FROM records;"}}, Options{}, database)
	if err == nil || !strings.Contains(err.Error(), "local primary key [id name] differs from database primary key [name id]") {
		t.Fatalf("primary key order diagnostic = %v", err)
	}
	if len(database.validated) != 1 {
		t.Fatal("query did not reach server validation before primary key drift check")
	}
}

func TestDatabaseAnalysisRejectsUnsupportedSourcesBeforeDiscovery(t *testing.T) {
	for _, sql := range []string{
		"-- name: Read :one\nSELECT id FROM (SELECT id FROM records) AS r;",
		"-- name: Read :one\nWITH r AS (SELECT id FROM records) SELECT id FROM r;",
		"-- name: Read :one\nSELECT id FROM $table;",
		"-- name: Read :one\nSELECT id FROM cluster.records;",
		"-- name: Bad :exec\nCREATE TABLE records (id Uint64, PRIMARY KEY(id));",
		"-- name: Bad :one\nSELECT id FROM records; SELECT id FROM records;",
		"-- name: Bad :one\nSELECT sqlc.arg(id) FROM records;",
		"-- name: Bad :one\nDECLARE $id AS Mystery; SELECT id FROM records;",
	} {
		t.Run(sql, func(t *testing.T) {
			database := &fakeAnalysisDatabase{}
			_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
			if err == nil || len(database.described) != 0 || !reflect.DeepEqual(database.validated, []string{sql}) {
				t.Fatalf("err=%v describes=%v validations=%v", err, database.described, database.validated)
			}
		})
	}
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
		{"server compilation", fakeAnalysisDatabase{tables: map[string]model.Table{"records": databaseTestTable("name")}, validateError: errors.New("server compilation failed")}, "database query validation failed: server compilation failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := AnalyzeWithDatabase(context.Background(), nil, queries, Options{}, &test.database)
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "query.sql:") {
				t.Fatalf("diagnostic=%v, want %q", err, test.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	database := &fakeAnalysisDatabase{}
	_, err := AnalyzeWithDatabase(ctx, nil, queries, Options{}, database)
	if err == nil || !strings.Contains(err.Error(), "context canceled") || len(database.described) != 0 || len(database.validated) != 0 {
		t.Fatalf("canceled analysis: %v, calls %v", err, database.described)
	}
}

func TestDatabaseAnalysisKeepsOfflineExpressionLimits(t *testing.T) {
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": databaseTestTable("name")}}
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT id / 1 AS next FROM records;"}}, Options{}, database)
	if err == nil || !strings.Contains(err.Error(), "unsupported arithmetic operator") || len(database.validated) != 1 {
		t.Fatalf("unresolved expression bypassed semantic analysis: %v, calls %v", err, database.validated)
	}
}
