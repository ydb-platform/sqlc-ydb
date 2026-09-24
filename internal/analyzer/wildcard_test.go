package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func TestAnalyzeExpandsWildcardSQL(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (z Utf8, id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id));"}}
	for _, test := range []struct {
		name, sql, want string
	}{
		{"plain", "SELECT * FROM records;", "SELECT `z`, `id`, `name` FROM records;"},
		{"qualified", "SELECT r.* FROM records AS r;", "SELECT r.`z` AS `z`, `r`.`id` AS `id`, `r`.`name` AS `name` FROM records AS r;"},
		{"returning", "DELETE FROM records WHERE id = $id RETURNING *;", "DELETE FROM records WHERE id = $id RETURNING `z`, `id`, `name`;"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prefix := "-- name: Read :many\n"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: prefix + test.sql}})
			require.NoError(t, err)
			query := result.Queries[0]
			require.Equal(t, prefix+test.want, query.SQL)
			require.Equal(t, result.Catalog.Tables[0].Columns, query.ResultSets[0].Columns)
		})
	}
}

func TestDatabaseWildcardExpansionPreservesCatalogOrderAndValidationSQL(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "discovery", true: "local"}[local], func(t *testing.T) {
			var schema []model.Source
			if local {
				schema = []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (name Utf8 NOT NULL, id Uint64 NOT NULL, PRIMARY KEY(id));"}}
			}
			remote := databaseTestTable("name")
			queries := []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT * FROM records;"}}
			database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": remote}}
			result, err := AnalyzeWithDatabase(context.Background(), schema, queries, Options{}, database)
			require.NoError(t, err)
			wantSQL := "-- name: Read :many\nSELECT `id`, `name` FROM records;"
			if local {
				wantSQL = "-- name: Read :many\nSELECT `name`, `id` FROM records;"
			}
			require.Equal(t, wantSQL, result.Queries[0].SQL)
			require.Equal(t, []string{queries[0].Text}, database.validated)
			require.Equal(t, result.Catalog.Tables[0].Columns, result.Queries[0].ResultSets[0].Columns)
		})
	}
}

func TestWildcardRewriteKeepsSyntaxAndBindingsAligned(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `путь/records` (z Utf8, id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	sql := "-- name: Read :many\r\nDECLARE $id AS Uint64;\r\n-- Юникод * не проекция\r\nSELECT /* before */ r /* qualifier */ . * /* after */\r\nFROM `путь/records` AS r WHERE r.id = $id;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	query := result.Queries[0]
	want := strings.Replace(sql, ". *", ". `z` AS `z`, `r`.`id` AS `id`", 1)
	require.Equal(t, want, query.SQL)
	require.Equal(t, []string{"id"}, query.DeclaredParameters)
	require.Equal(t, 1, query.Source.Line)
	var matchedTable, matchedBinding bool
	descendants(query.Syntax.Root, func(node antlr.Tree) {
		if ref, ok := node.(*parser.Table_keyContext); ok {
			start, end := runeByteOffset(query.SQL, ref.GetStart().GetStart()), runeByteOffset(query.SQL, ref.GetStop().GetStop()+1)
			assert.Equal(t, "`путь/records`", query.SQL[start:end])
			matchedTable = true
		}
		if expression, ok := node.(*parser.Unary_subexprContext); ok && expression.GetText() == "r.id" {
			binding, found := query.Syntax.Columns[expression.GetStart().GetTokenIndex()]
			assert.False(t, !found || binding.Column.Name != "id" || binding.Table != "путь/records")
			matchedBinding = true
		}
	})
	require.True(t, matchedTable)
	require.True(t, matchedBinding)
}

func TestAnalyzeExpandsDMLSelectWildcards(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (z Utf8 NOT NULL, id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	for _, statement := range []string{"UPDATE records", "DELETE FROM records"} {
		for _, aliased := range []bool{false, true} {
			t.Run(statement+map[bool]string{false: "/unqualified", true: "/qualified"}[aliased], func(t *testing.T) {
				projection, expanded, source := "*", "`z`, `id`", "AS_TABLE($rows)"
				if aliased {
					projection, expanded, source = "r.*", "r.`z` AS `z`, `r`.`id` AS `id`", source+" AS r"
				}
				prefix := "-- name: Change :many\nDECLARE $rows AS List<Struct<z:Utf8,id:Uint64>>;\n" + statement + " ON SELECT "
				sql := prefix + projection + " FROM " + source + " RETURNING *;"
				want := prefix + expanded + " FROM " + source + " RETURNING `z`, `id`;"
				result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
				require.NoError(t, err)
				query := result.Queries[0]
				require.Equal(t, want, query.SQL)
				require.Equal(t, result.Catalog.Tables[0].Columns, query.ResultSets[0].Columns)
				require.Len(t, query.Parameters, 1)
				require.Equal(t, "List", query.Parameters[0].Type.Kind)
			})
		}
	}
}

func TestAnalyzeExpandsUnionWildcards(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (z Utf8, id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	sql := "-- name: Read :many\nSELECT * FROM records\nUNION ALL\nSELECT r.* FROM records r;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	want := "-- name: Read :many\nSELECT `z`, `id` FROM records\nUNION ALL\nSELECT r.`z` AS `z`, `r`.`id` AS `id` FROM records r;"
	{
		query := result.Queries[0]
		require.Equal(t, want, query.SQL)
		require.Equal(t, "z", query.ResultSets[0].Columns[0].Name)
		require.Equal(t, "id", query.ResultSets[0].Columns[1].Name)
	}
}

func TestAnalyzeWildcardJoinResultKeys(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE first_table (z Utf8 NOT NULL, key Uint64 NOT NULL, PRIMARY KEY(key)); CREATE TABLE second_table (name Utf8 NOT NULL, id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	for _, test := range []struct {
		projection, expanded string
		names                []string
	}{
		{"*", "`a`.`z` AS `z`, `a`.`key` AS `key`, `b`.`name` AS `name`, `b`.`id` AS `id`", []string{"z", "key", "name", "id"}},
		{"b.*", "b.`name` AS `name`, `b`.`id` AS `id`", []string{"name", "id"}},
	} {
		t.Run(test.projection, func(t *testing.T) {
			prefix, suffix := "-- name: Read :many\nSELECT ", " FROM first_table a LEFT JOIN second_table b ON a.key = b.id;"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: prefix + test.projection + suffix}})
			require.NoError(t, err)
			query := result.Queries[0]
			require.Equal(t, prefix+test.expanded+suffix, query.SQL)
			for i, column := range query.ResultSets[0].Columns {
				require.Equal(t, test.names[i], column.Name)
				require.Equal(t, test.names[i], column.ResultName())
				require.Equal(t, (column.Table == "second_table"), column.Type.IsOptional())
			}
		})
	}
}

func TestAnalyzeWildcardQuotedIdentifiersAndLiteralBytes(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `путь/records` (`z``value` Utf8, `ключ` Uint64 NOT NULL, PRIMARY KEY(`ключ`));"}}
	sql := "-- name: Read :many\r\n-- * \x01\t юникод\r\nSELECT `строка` . *, '*' AS marker /* literal * */ FROM `путь/records` AS `строка`;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	want := strings.Replace(sql, ". *", ". `z``value` AS `z``value`, `строка`.`ключ` AS `ключ`", 1)
	query := result.Queries[0]
	require.Equal(t, want, query.SQL)
	{
		columns := query.ResultSets[0].Columns
		require.Equal(t, "z`value", columns[0].Name)
		require.Equal(t, "ключ", columns[1].Name)
		require.Equal(t, "", columns[0].WireName)
		require.Equal(t, "", columns[1].WireName)
	}
}

func TestAnalyzeLeavesNonProjectionAsterisksUnchanged(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	sql := "-- name: Count :one\nSELECT COUNT(*) AS total FROM records;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	require.Equal(t, sql, result.Queries[0].SQL)
	sql = "-- name: Multiply :many\nSELECT id * id AS product FROM records;"
	result, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	require.Equal(t, sql, result.Queries[0].SQL)
	require.Equal(t, "Uint64", result.Queries[0].ResultSets[0].Columns[0].Type.Kind)
}

func TestAnalyzeWildcardJoinRequiresAS_TABLEAlias(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	sql := "-- name: Read :many\nDECLARE $rows AS List<Struct<x:Uint64>>;\nSELECT * FROM AS_TABLE($rows) CROSS JOIN records AS r;"
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.ErrorContains(t, err, "AS_TABLE in a join requires an explicit alias")
	sql = strings.Replace(sql, "AS_TABLE($rows) CROSS", "AS_TABLE($rows) AS a CROSS", 1)
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	{
		want := strings.Replace(sql, "SELECT *", "SELECT `a`.`x` AS `x`, `r`.`id` AS `id`", 1)
		require.Equal(t, want, result.Queries[0].SQL)
	}
}
