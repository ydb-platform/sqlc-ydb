package analyzer

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func TestTablePathPrefixResolvesDistinctNamespaces(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/one/records` (id Uint32 NOT NULL, PRIMARY KEY(id)); CREATE TABLE `/local/two/records` (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	for _, tc := range []struct{ prefix, kind string }{{"/local/one", "Uint32"}, {"/local/two", "Uint64"}} {
		t.Run(tc.prefix, func(t *testing.T) {
			sql := "-- name: Read :many\nPRAGMA TablePathPrefix('" + tc.prefix + "');\nSELECT records.id FROM records WHERE records.id = $id;"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
			if err != nil {
				t.Fatal(err)
			}
			query := result.Queries[0]
			want := []model.Parameter{{Name: "id", Type: model.Type{Kind: tc.kind}}}
			if query.SQL != sql || !reflect.DeepEqual(query.Parameters, want) {
				t.Fatalf("query = %#v", query)
			}
			if !reflect.DeepEqual(query.Syntax.Relations, []model.TableBinding{{Table: tc.prefix + "/records", Alias: "records"}}) {
				t.Fatalf("relations = %#v", query.Syntax.Relations)
			}
			column := query.ResultSets[0].Columns[0]
			if column.Type.Kind != tc.kind || column.Table != tc.prefix+"/records" {
				t.Fatalf("column = %#v", column)
			}
		})
	}
}

func TestTablePathPrefixSchemaSourceScope(t *testing.T) {
	schema := []model.Source{
		{Name: "one.sql", Text: "PRAGMA TablePathPrefix('/local/one'); CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"},
		{Name: "two.sql", Text: "PRAGMA TablePathPrefix = '/local/two'; CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"},
		{Name: "root.sql", Text: "CREATE TABLE records (id Utf8 NOT NULL, PRIMARY KEY(id));"},
	}
	result, err := Analyze(schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, table := range result.Catalog.Tables {
		names = append(names, table.Name)
	}
	if !reflect.DeepEqual(names, []string{"/local/one/records", "/local/two/records", "records"}) {
		t.Fatalf("catalog names = %v", names)
	}
}

func TestTablePathPrefixStaticFormsAndPaths(t *testing.T) {
	for _, tc := range []struct{ pragma, source, path string }{
		{`PRAGMA TablePathPrefix('/local/one');`, "records", "/local/one/records"},
		{`pragma tablepathprefix = "/local/one";`, "records", "/local/one/records"},
		{`PRAGMA TablePathPrefix('/local/one'); PRAGMA TablePathPrefix('/local/one');`, "records", "/local/one/records"},
		{`PRAGMA TablePathPrefix('/local/one/./nested/../');`, "`./records`", "/local/one/records"},
		{`PRAGMA TablePathPrefix('/local/one');`, "`../two//records`", "/local/two/records"},
		{`PRAGMA TablePathPrefix('/local/one');`, "`/local/two/records`", "/local/two/records"},
		{`PRAGMA TablePathPrefix('/');`, "`local/two/records`", "/local/two/records"},
		{`PRAGMA TablePathPrefix('\x2flocal\u002fone');`, "records", "/local/one/records"},
		{`PRAGMA TablePathPrefix('\057local/one');`, "records", "/local/one/records"},
		{`PRAGMA TablePathPrefix('/local/имя');`, "`rec``ords`", "/local/имя/rec`ords"},
		{`PRAGMA TablePathPrefix('/local/quo\"te');`, "records", "/local/quo\"te/records"},
		{`PRAGMA TablePathPrefix("/local/quo\'te");`, "records", "/local/quo'te/records"},
	} {
		t.Run(tc.pragma+tc.source, func(t *testing.T) {
			schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE " + quotedYQLIdentifier(tc.path) + " (id Uint32 NOT NULL, PRIMARY KEY(id));"}}
			sql := "-- name: Read :many\n" + tc.pragma + "\n-- records and /local/one stay unchanged\nSELECT id FROM " + tc.source + ";"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
			if err != nil {
				t.Fatal(err)
			}
			query := result.Queries[0]
			if query.SQL != sql || query.ResultSets[0].Columns[0].Table != tc.path {
				t.Fatalf("query = %#v", query)
			}
			if len(query.Syntax.Tables) != 1 {
				t.Fatalf("table mappings = %v", query.Syntax.Tables)
			}
			for _, name := range query.Syntax.Tables {
				if name != tc.path {
					t.Fatalf("physical path = %q", name)
				}
			}
		})
	}
}

func TestTablePathPrefixQueryScope(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/one/records` (id Uint32 NOT NULL, PRIMARY KEY(id)); CREATE TABLE records (id Utf8 NOT NULL, PRIMARY KEY(id));"}}
	queries := []model.Source{{Name: "query.sql", Text: "-- name: First :many\nPRAGMA TablePathPrefix('/local/one'); SELECT id FROM records;\n-- name: Second :many\nSELECT id FROM records;"}}
	result, err := Analyze(schema, queries)
	if err != nil {
		t.Fatal(err)
	}
	if result.Queries[0].ResultSets[0].Columns[0].Type.Kind != "Uint32" || result.Queries[1].ResultSets[0].Columns[0].Type.Kind != "Utf8" || result.Queries[1].Syntax.TablePathPrefix != "" {
		t.Fatalf("queries = %#v", result.Queries)
	}
}

func TestTablePathPrefixMigrations(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `PRAGMA TablePathPrefix('/local/one');
CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));
CREATE TABLE IF NOT EXISTS records (ignored Uint32, PRIMARY KEY(ignored));
ALTER TABLE records ADD COLUMN label Utf8;
ALTER TABLE records RENAME TO renamed;
ALTER TABLE renamed RENAME TO ` + "`/local/two/final`" + `;
CREATE TABLE disposable (id Uint32 NOT NULL, PRIMARY KEY(id));
DROP TABLE disposable;
DROP TABLE IF EXISTS disposable;
ALTER TABLE ` + "`/local/two/final`" + ` ADD INDEX by_label GLOBAL SYNC ON(label);`}}
	result, err := Analyze(schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Catalog.Tables) != 1 {
		t.Fatalf("tables = %#v", result.Catalog.Tables)
	}
	table := result.Catalog.Tables[0]
	if table.Name != "/local/two/final" || len(table.Columns) != 2 || len(table.Indexes) != 1 {
		t.Fatalf("table = %#v", table)
	}
	for _, column := range table.Columns {
		if column.Table != table.Name {
			t.Fatalf("column ownership = %#v", column)
		}
	}
}

func TestTablePathPrefixDMLAndJoins(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "PRAGMA TablePathPrefix('/local/one'); CREATE TABLE records (id Uint32 NOT NULL, label Utf8, INDEX by_label GLOBAL SYNC ON(label), PRIMARY KEY(id)); CREATE TABLE `/local/two/records` (id Uint32 NOT NULL, label Utf8, PRIMARY KEY(id));"}}
	for _, sql := range []string{
		"INSERT INTO records (id, label) VALUES ($id, $label);",
		"UPSERT INTO records (id, label) SELECT id, label FROM `/local/two/records`;",
		"UPSERT INTO records SELECT * FROM `/local/two/records`;",
		"UPDATE records SET label = $label WHERE records.id = $id;",
		"UPDATE records SET id = records.id + 1u WHERE records.id = $id;",
		"DELETE FROM records WHERE records.id = $id;",
		"UPDATE records ON SELECT id, label FROM `/local/two/records`;",
		"DELETE FROM records ON SELECT id FROM `/local/two/records`;",
	} {
		t.Run(sql, func(t *testing.T) {
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\nPRAGMA TablePathPrefix('/local/one'); " + sql}})
			if err != nil {
				t.Fatal(err)
			}
			query := result.Queries[0]
			var paths []string
			for _, name := range query.Syntax.Tables {
				paths = append(paths, name)
			}
			if !slices.Contains(paths, "/local/one/records") {
				t.Fatalf("target mapping = %v", paths)
			}
			if strings.Contains(sql, "SELECT") && !slices.Contains(paths, "/local/two/records") {
				t.Fatalf("source mapping = %v", paths)
			}
		})
	}
	for _, projection := range []string{"r.*", "r.id, s.label"} {
		sql := "-- name: Joined :many\nPRAGMA TablePathPrefix('/local/one'); SELECT " + projection + " FROM records VIEW by_label AS r JOIN `/local/two/records` AS s ON r.id = s.id;"
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		if err != nil {
			t.Fatal(err)
		}
		query := result.Queries[0]
		want := []model.TableBinding{{Table: "/local/one/records", Alias: "r"}, {Table: "/local/two/records", Alias: "s"}}
		if !reflect.DeepEqual(query.Syntax.Relations, want) {
			t.Fatalf("relations = %#v", query.Syntax.Relations)
		}
		columns := query.ResultSets[0].Columns
		if projection == "r.*" && (columns[0].ResultName() != "id" || columns[1].ResultName() != "label") {
			t.Fatalf("wildcard keys = %#v", columns)
		}
		if projection != "r.*" && (columns[0].ResultName() != "r.id" || columns[1].ResultName() != "s.label") {
			t.Fatalf("qualified keys = %#v", columns)
		}
	}
}

func TestTablePathPrefixUsesAuthoredQualifier(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/one/nested/records` (id Uint32 NOT NULL, PRIMARY KEY(id));"}}
	for _, qualifier := range []string{"`nested/records`", "records", "`/local/one/nested/records`"} {
		sql := "-- name: Read :many\nPRAGMA TablePathPrefix('/local/one'); SELECT " + qualifier + ".id FROM `nested/records`;"
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		if qualifier == "`nested/records`" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "unknown column") {
			t.Fatalf("qualifier %s: %v", qualifier, err)
		}
	}
}

func TestTablePathPrefixRejectsUnsupportedPragmas(t *testing.T) {
	for _, tc := range []struct{ sql, message string }{
		{`PRAGMA TablePathPrefix('');`, "nonempty absolute path"},
		{`PRAGMA TablePathPrefix('local/one');`, "nonempty absolute path"},
		{`PRAGMA TablePathPrefix($prefix);`, "static quoted absolute path"},
		{`PRAGMA TablePathPrefix = default;`, "reset forms are unsupported"},
		{`PRAGMA TablePathPrefix;`, "static quoted absolute path"},
		{`PRAGMA TablePathPrefix('/one', '/two');`, "one static quoted absolute path"},
		{`PRAGMA TablePathPrefix('/one'); PRAGMA TablePathPrefix('/two');`, "conflicting PRAGMA"},
		{`PRAGMA TablePathPrefix('/local' || '/one');`, "input"},
		{`PRAGMA TablePathPrefix('/local' u);`, "input"},
		{`PRAGMA TablePathPrefix("/local"u);`, "ordinary quoted string"},
		{`PRAGMA TablePathPrefix('/local\n/one');`, "control character"},
		{`PRAGMA TablePathPrefix('/local\q/one');`, "unsupported escape"},
		{`PRAGMA ydb.TablePathPrefix('/local/one');`, "only static TablePathPrefix"},
		{`PRAGMA AnsiInForEmptyOrNullableItemsCollections;`, "only static TablePathPrefix"},
		{`$x = 1; PRAGMA TablePathPrefix('/local/one');`, "must precede"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.sql + "SELECT id FROM records;"}})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("error = %v; want %q", err, tc.message)
			}
			_, err = Analyze([]model.Source{{Name: "schema.sql", Text: tc.sql + "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"}}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("schema error = %v; want %q", err, tc.message)
			}
		})
	}
	for _, statement := range []string{"SELECT id FROM records;", "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"} {
		sql := statement + " PRAGMA TablePathPrefix('/local/one');"
		parsed, ds := parseYQL("query.sql", sql, 0)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		_, ds = tablePathPrefix("query.sql", 0, parsed.tree)
		if len(ds) != 1 || !strings.Contains(ds[0].Message, "must precede") {
			t.Fatalf("late pragma diagnostics = %v", ds)
		}
	}
}

func TestTablePathPrefixDatabaseDiscoveryAndDrift(t *testing.T) {
	for _, withSchema := range []bool{false, true} {
		t.Run(fmt.Sprint(withSchema), func(t *testing.T) {
			database := &fakeAnalysisDatabase{tables: map[string]model.Table{"/local/one/records": databaseTestTable("name"), "/local/two/records": databaseTestTable("name")}}
			queries := []model.Source{{Name: "query.sql", Text: "-- name: First :many\nPRAGMA TablePathPrefix('/local/one'); SELECT id FROM records;\n-- name: Second :exec\nPRAGMA TablePathPrefix('/local/two'); UPSERT INTO records SELECT id, name FROM `/local/one/records`;"}}
			var schema []model.Source
			if withSchema {
				for _, prefix := range []string{"/local/one", "/local/two"} {
					schema = append(schema, model.Source{Name: prefix + ".sql", Text: "PRAGMA TablePathPrefix('" + prefix + "'); CREATE TABLE records (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id));"})
				}
			}
			result, err := AnalyzeWithDatabase(context.Background(), schema, queries, Options{}, database)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(database.described, []string{"/local/one/records", "/local/two/records"}) || len(result.Catalog.Tables) != 2 {
				t.Fatalf("described=%v tables=%#v", database.described, result.Catalog.Tables)
			}
			if len(database.validated) != 2 || !strings.Contains(database.validated[1], "PRAGMA TablePathPrefix('/local/two')") {
				t.Fatalf("validated=%v", database.validated)
			}
			if withSchema {
				table := database.tables["/local/two/records"]
				table.Columns[0].Type = model.Type{Kind: "Uint32"}
				database.tables["/local/two/records"] = table
				_, err = AnalyzeWithDatabase(context.Background(), schema, queries, Options{}, database)
				if err == nil || !strings.Contains(err.Error(), `database schema drift for table "/local/two/records"`) {
					t.Fatalf("drift = %v", err)
				}
			}
		})
	}
}

func TestTablePathPrefixPreservesTextAndDMLReturning(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/one/records` (id Uint32 NOT NULL, PRIMARY KEY(id));"}}
	sql := "-- name: Read :many\r\nPRAGMA /* records */ TablePathPrefix = '/local/one';\r\nDECLARE $id AS Uint32;\r\nSELECT records.id, 'records /local/one PRAGMA' AS marker FROM records WHERE records.id = $id;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Queries[0].SQL != sql {
		t.Fatalf("SQL changed: %q", result.Queries[0].SQL)
	}
	for _, statement := range []string{"INSERT INTO records (id) VALUES ($id)", "UPSERT INTO records (id) VALUES ($id)", "UPDATE records SET id = records.id + 1u WHERE records.id = $id", "DELETE FROM records WHERE records.id = $id"} {
		sql = "-- name: Write :many\nPRAGMA TablePathPrefix('/local/one'); " + statement + " RETURNING *;"
		result, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		if err != nil {
			t.Fatal(err)
		}
		query := result.Queries[0]
		if !strings.Contains(query.SQL, "RETURNING `id`") || !strings.Contains(query.SQL, "PRAGMA TablePathPrefix('/local/one')") || query.ResultSets[0].Columns[0].Table != "/local/one/records" {
			t.Fatalf("query = %#v", query)
		}
	}
}

func TestTablePathPrefixDatabaseRejectsUnsupportedBeforeDiscovery(t *testing.T) {
	database := &fakeAnalysisDatabase{}
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nPRAGMA TablePathPrefix($prefix); SELECT id FROM records;"}}, Options{}, database)
	if err == nil || !strings.Contains(err.Error(), "static quoted absolute path") || len(database.described) != 0 {
		t.Fatalf("error=%v described=%v", err, database.described)
	}
}

func TestTablePathPrefixDoesNotEnableAnsiDialect(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nPRAGMA ANSI 1;"}})
	if err == nil || !strings.Contains(err.Error(), "named query requires a SELECT") {
		t.Fatalf("error = %v", err)
	}
}

func TestTableAliasIsTheOnlyQualifier(t *testing.T) {
	for _, prefix := range []string{"", "/local/one"} {
		t.Run(prefix, func(t *testing.T) {
			pragma := ""
			if prefix != "" {
				pragma = "PRAGMA TablePathPrefix('" + prefix + "'); "
			}
			schema := []model.Source{{Name: "schema.sql", Text: pragma + "CREATE TABLE records (id Uint32 NOT NULL, meta Struct<value:Uint32> NOT NULL, PRIMARY KEY(id));"}}
			for _, tc := range []struct{ expression, error string }{
				{"records.id", "unknown column"},
				{"records.*", "unknown table or alias"},
				{"records.meta.value AS value", "unknown column"},
				{"R.id", "unknown column"},
				{"R.*", "unknown table or alias"},
				{"r.id", ""},
				{"r.*", ""},
				{"r.meta.value AS value", ""},
			} {
				_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + pragma + "SELECT " + tc.expression + " FROM records AS r;"}})
				if tc.error == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tc.error) {
					t.Fatalf("expression %q: error=%v want=%s", tc.expression, err, tc.error)
				}
			}
		})
	}
}

func TestColumnBindingsPreserveAliasCase(t *testing.T) {
	for _, prefix := range []string{"", "/local/one"} {
		t.Run(prefix, func(t *testing.T) {
			pragma := ""
			if prefix != "" {
				pragma = "PRAGMA TablePathPrefix('" + prefix + "'); "
			}
			schema := []model.Source{{Name: "schema.sql", Text: pragma + "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id)); CREATE TABLE other_records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
			sql := "-- name: Read :many\n" + pragma + "SELECT r.id AS lower_id, R.id AS upper_id FROM records AS r JOIN other_records AS R ON r.id = R.id WHERE R.id = $id;"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
			if err != nil {
				t.Fatal(err)
			}
			query := result.Queries[0]
			if query.ResultSets[0].Columns[0].Type.Kind != "Uint32" || query.ResultSets[0].Columns[1].Type.Kind != "Uint64" || !reflect.DeepEqual(query.Parameters, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}) {
				t.Fatalf("query types = %#v, parameters = %#v", query.ResultSets, query.Parameters)
			}
			var core *parser.Select_coreContext
			descendants(query.Syntax.Root, func(node antlr.Tree) {
				if selectCore, ok := node.(*parser.Select_coreContext); ok {
					core = selectCore
				}
			})
			bindings := 0
			for _, ref := range columnRefs(core) {
				wantTable, wantType := "records", "Uint32"
				if ref.qualifier == "R" {
					wantTable, wantType = "other_records", "Uint64"
				}
				if prefix != "" {
					wantTable = prefix + "/" + wantTable
				}
				binding, ok := query.Syntax.Columns[ref.ctx.GetStart().GetTokenIndex()]
				if !ok || binding.Alias != ref.qualifier || binding.Table != wantTable || binding.Column.Type.Kind != wantType {
					t.Errorf("binding for %s = %#v, want alias %q, table %q, type %s", qualifiedName(ref), binding, ref.qualifier, wantTable, wantType)
				}
				bindings++
			}
			if bindings != 5 {
				t.Fatalf("checked %d column bindings, want 5", bindings)
			}
		})
	}
}

func TestTablePathPrefixDeclarationAndAssignmentOrdering(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/one/records` (id Uint32 NOT NULL, PRIMARY KEY(id));"}}
	const declaration = "DECLARE $id AS Uint32; "
	const pragma = "PRAGMA TablePathPrefix('/local/one'); "
	const assignment = "$key = $id; "
	for _, preamble := range []string{declaration + pragma, pragma + declaration, pragma + declaration + pragma} {
		sql := "-- name: Read :many\n" + preamble + assignment + "SELECT id FROM records WHERE id = $key;"
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		if err != nil {
			t.Fatal(err)
		}
		query := result.Queries[0]
		if query.SQL != sql || !reflect.DeepEqual(query.Parameters, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint32"}}}) {
			t.Fatalf("query = %#v", query)
		}
	}
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + declaration + assignment + pragma + "SELECT id FROM records WHERE id = $key;"}})
	if err == nil || !strings.Contains(err.Error(), "must precede local assignments ($name = ...)") || !strings.Contains(err.Error(), "DECLARE may precede the pragma") {
		t.Fatalf("late pragma error = %v", err)
	}
}
