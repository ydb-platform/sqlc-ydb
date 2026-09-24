package analyzer

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			require.NoError(t, err)
			query := result.Queries[0]
			want := []model.Parameter{{Name: "id", Type: model.Type{Kind: tc.kind}}}
			require.Equal(t, sql, query.SQL)
			require.Equal(t, want, query.Parameters)
			require.Equal(t, []model.TableBinding{{Table: tc.prefix + "/records", Alias: "records"}}, query.Syntax.Relations)
			column := query.ResultSets[0].Columns[0]
			require.Equal(t, tc.kind, column.Type.Kind)
			require.Equal(t, tc.prefix+"/records", column.Table)
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
	require.NoError(t, err)
	var names []string
	for _, table := range result.Catalog.Tables {
		names = append(names, table.Name)
	}
	require.Equal(t, []string{"/local/one/records", "/local/two/records", "records"}, names)
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
			require.NoError(t, err)
			query := result.Queries[0]
			require.Equal(t, sql, query.SQL)
			require.Equal(t, tc.path, query.ResultSets[0].Columns[0].Table)
			require.Len(t, query.Syntax.Tables, 1)
			for _, name := range query.Syntax.Tables {
				require.Equal(t, tc.path, name)
			}
		})
	}
}

func TestTablePathPrefixQueryScope(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/one/records` (id Uint32 NOT NULL, PRIMARY KEY(id)); CREATE TABLE records (id Utf8 NOT NULL, PRIMARY KEY(id));"}}
	queries := []model.Source{{Name: "query.sql", Text: "-- name: First :many\nPRAGMA TablePathPrefix('/local/one'); SELECT id FROM records;\n-- name: Second :many\nSELECT id FROM records;"}}
	result, err := Analyze(schema, queries)
	require.NoError(t, err)
	require.Equal(t, "Uint32", result.Queries[0].ResultSets[0].Columns[0].Type.Kind)
	require.Equal(t, "Utf8", result.Queries[1].ResultSets[0].Columns[0].Type.Kind)
	require.Equal(t, "", result.Queries[1].Syntax.TablePathPrefix)
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
	require.NoError(t, err)
	require.Len(t, result.Catalog.Tables, 1)
	table := result.Catalog.Tables[0]
	require.Equal(t, "/local/two/final", table.Name)
	require.Len(t, table.Columns, 2)
	require.Len(t, table.Indexes, 1)
	for _, column := range table.Columns {
		require.Equal(t, table.Name, column.Table)
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
			require.NoError(t, err)
			query := result.Queries[0]
			var paths []string
			for _, name := range query.Syntax.Tables {
				paths = append(paths, name)
			}
			require.True(t, slices.Contains(paths, "/local/one/records"))
			require.False(t, strings.Contains(sql, "SELECT") && !slices.Contains(paths, "/local/two/records"))
		})
	}
	for _, projection := range []string{"r.*", "r.id, s.label"} {
		sql := "-- name: Joined :many\nPRAGMA TablePathPrefix('/local/one'); SELECT " + projection + " FROM records VIEW by_label AS r JOIN `/local/two/records` AS s ON r.id = s.id;"
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		require.NoError(t, err)
		query := result.Queries[0]
		want := []model.TableBinding{{Table: "/local/one/records", Alias: "r"}, {Table: "/local/two/records", Alias: "s"}}
		require.Equal(t, want, query.Syntax.Relations)
		columns := query.ResultSets[0].Columns
		require.False(t, projection == "r.*" && (columns[0].ResultName() != "id" || columns[1].ResultName() != "label"))
		require.False(t, projection != "r.*" && (columns[0].ResultName() != "r.id" || columns[1].ResultName() != "s.label"))
	}
}

func TestTablePathPrefixUsesAuthoredQualifier(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/one/nested/records` (id Uint32 NOT NULL, PRIMARY KEY(id));"}}
	for _, qualifier := range []string{"`nested/records`", "records", "`/local/one/nested/records`"} {
		sql := "-- name: Read :many\nPRAGMA TablePathPrefix('/local/one'); SELECT " + qualifier + ".id FROM `nested/records`;"
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		if qualifier == "`nested/records`" {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "unknown column")
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
			require.ErrorContains(t, err, tc.message)
			_, err = Analyze([]model.Source{{Name: "schema.sql", Text: tc.sql + "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"}}, nil)
			require.ErrorContains(t, err, tc.message)
		})
	}
	for _, statement := range []string{"SELECT id FROM records;", "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"} {
		sql := statement + " PRAGMA TablePathPrefix('/local/one');"
		parsed, ds := parseYQL("query.sql", sql, 0)
		require.Len(t, ds, 0)
		_, ds = tablePathPrefix("query.sql", 0, parsed.tree)
		require.Len(t, ds, 1)
		require.Contains(t, ds[0].Message, "must precede")
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
			require.NoError(t, err)
			require.Equal(t, []string{"/local/one/records", "/local/two/records"}, database.described)
			require.Len(t, result.Catalog.Tables, 2)
			require.Len(t, database.validated, 2)
			require.Contains(t, database.validated[1], "PRAGMA TablePathPrefix('/local/two')")
			if withSchema {
				table := database.tables["/local/two/records"]
				table.Columns[0].Type = model.Type{Kind: "Uint32"}
				database.tables["/local/two/records"] = table
				_, err = AnalyzeWithDatabase(context.Background(), schema, queries, Options{}, database)
				require.ErrorContains(t, err, `database schema drift for table "/local/two/records"`)
			}
		})
	}
}

func TestTablePathPrefixPreservesTextAndDMLReturning(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/one/records` (id Uint32 NOT NULL, PRIMARY KEY(id));"}}
	sql := "-- name: Read :many\r\nPRAGMA /* records */ TablePathPrefix = '/local/one';\r\nDECLARE $id AS Uint32;\r\nSELECT records.id, 'records /local/one PRAGMA' AS marker FROM records WHERE records.id = $id;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	require.Equal(t, sql, result.Queries[0].SQL)
	for _, statement := range []string{"INSERT INTO records (id) VALUES ($id)", "UPSERT INTO records (id) VALUES ($id)", "UPDATE records SET id = records.id + 1u WHERE records.id = $id", "DELETE FROM records WHERE records.id = $id"} {
		sql = "-- name: Write :many\nPRAGMA TablePathPrefix('/local/one'); " + statement + " RETURNING *;"
		result, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		require.NoError(t, err)
		query := result.Queries[0]
		require.Contains(t, query.SQL, "RETURNING `id`")
		require.Contains(t, query.SQL, "PRAGMA TablePathPrefix('/local/one')")
		require.Equal(t, "/local/one/records", query.ResultSets[0].Columns[0].Table)
	}
}

func TestTablePathPrefixDatabaseRejectsUnsupportedBeforeDiscovery(t *testing.T) {
	database := &fakeAnalysisDatabase{}
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nPRAGMA TablePathPrefix($prefix); SELECT id FROM records;"}}, Options{}, database)
	require.Error(t, err)
	require.Contains(t, err.Error(), "static quoted absolute path")
	require.Len(t, database.described, 0)
}

func TestTablePathPrefixDoesNotEnableAnsiDialect(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nPRAGMA ANSI 1;"}})
	require.ErrorContains(t, err, "named query requires a SELECT")
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
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tc.error)
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
			require.NoError(t, err)
			query := result.Queries[0]
			require.Equal(t, "Uint32", query.ResultSets[0].Columns[0].Type.Kind)
			require.Equal(t, "Uint64", query.ResultSets[0].Columns[1].Type.Kind)
			require.Equal(t, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, query.Parameters)
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
				assert.False(t, !ok || binding.Alias != ref.qualifier || binding.Table != wantTable || binding.Column.Type.Kind != wantType)
				bindings++
			}
			require.Equal(t, 5, bindings)
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
		require.NoError(t, err)
		query := result.Queries[0]
		require.Equal(t, sql, query.SQL)
		require.Equal(t, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint32"}}}, query.Parameters)
	}
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + declaration + assignment + pragma + "SELECT id FROM records WHERE id = $key;"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must precede local assignments ($name = ...)")
	require.Contains(t, err.Error(), "DECLARE may precede the pragma")
}
