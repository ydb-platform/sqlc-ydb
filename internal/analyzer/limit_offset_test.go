package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestDeclaredLimitOffsetTypes(t *testing.T) {
	for _, tc := range []struct{ declared, want string }{
		{"Int8", "Int8"},
		{"Int16", "Int16"},
		{"Int32", "Int32"},
		{"Uint8", "Uint8"},
		{"Uint16", "Uint16"},
		{"Uint32", "Uint32"},
		{"Uint64", "Uint64"},
		{"Int", "Int32"},
		{"Integer", "Int32"},
		{"TinyInt", "Int8"},
		{"SmallInt", "Int16"},
		{"Optional<Int32>", "Optional<Int32>"},
		{"Int8?", "Optional<Int8>"},
		{"Int16?", "Optional<Int16>"},
		{"Uint8?", "Optional<Uint8>"},
		{"Uint16?", "Optional<Uint16>"},
		{"Uint32?", "Optional<Uint32>"},
		{"Uint64?", "Optional<Uint64>"},
	} {
		t.Run(tc.declared, func(t *testing.T) {
			sql := "-- name: Page :many\nDECLARE $count AS " + tc.declared + "; DECLARE $skip AS " + tc.declared + "; SELECT id FROM records LIMIT $count OFFSET $skip;"
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: sql}})
			require.NoError(t, err)
			query := result.Queries[0]
			require.Equal(t, sql, query.SQL)
			require.Len(t, query.Parameters, 2)
			for _, param := range query.Parameters {
				require.Equal(t, tc.want, param.Type.String())
				require.True(t, query.IsDeclaredParameter(param.Name))
			}
		})
	}
}

func TestIntegerTypeAliases(t *testing.T) {
	for _, tc := range []struct{ alias, want string }{
		{"TinyInt", "Int8"}, {"SmallInt", "Int16"}, {"Int", "Int32"}, {"Integer", "Int32"}, {"BigInt", "Int64"}, {"iNtEgEr", "Int32"},
	} {
		t.Run(tc.alias, func(t *testing.T) {
			schema := "CREATE TABLE records (id " + tc.alias + " NOT NULL, PRIMARY KEY(id));"
			sql := "-- name: Read :many\nDECLARE $id AS " + tc.alias + "; SELECT CAST($id AS " + tc.alias + ") AS value FROM records WHERE id = $id;"
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: sql}})
			require.NoError(t, err)
			query := result.Queries[0]
			require.Equal(t, sql, query.SQL)
			require.Equal(t, tc.want, query.Parameters[0].Type.String())
			require.Equal(t, tc.want, query.ResultSets[0].Columns[0].Type.String())
			require.Equal(t, tc.want, result.Catalog.Tables[0].Columns[0].Type.String())
			typ, err := parseType("List<Struct<value:" + tc.alias + "?>>")
			require.NoError(t, err)
			require.Equal(t, "List<Struct<`value`:Optional<"+tc.want+">>>", typ.String())
		})
	}
	for _, alias := range []string{"Uint", "Unsigned", "Long", "Short", "Byte"} {
		{
			_, err := parseType(alias)
			require.ErrorContains(t, err, "unsupported YQL type")
		}
	}
}

func TestLimitOffsetScalarExpressions(t *testing.T) {
	for _, sql := range []string{
		"SELECT id FROM records LIMIT 0 OFFSET 2u;",
		"SELECT id FROM records LIMIT 18446744073709551615ul;",
		"SELECT id FROM records LIMIT 9223372036854775807l OFFSET (1l);",
		"SELECT id FROM records LIMIT 2147483648;",
		"SELECT id FROM records LIMIT NULL OFFSET NULL;",
		"SELECT id FROM records LIMIT 1 + 1 OFFSET CAST(0 AS Uint32);",
		"SELECT id FROM records LIMIT CAST('COUNT(*)' AS Uint32);",
		"DECLARE $n AS Uint32; SELECT id FROM records LIMIT ($n) OFFSET $n + 1u;",
		"DECLARE $n AS Int; $local = $n; SELECT id FROM records LIMIT $local;",
		"$local = 2u; SELECT id FROM records LIMIT $local OFFSET 0;",
		"$local = NULL; SELECT id FROM records LIMIT $local;",
		"DECLARE $skip AS Uint16; DECLARE $count AS Uint32; SELECT id FROM records LIMIT $skip, $count;",
		"DECLARE $n AS Uint32; SELECT id FROM records LIMIT $n UNION ALL SELECT id FROM records WHERE id = $n;",
		"DECLARE $n AS Int; SELECT id FROM records UNION ALL SELECT id FROM records LIMIT $n;",
		"SELECT $n AS count FROM records LIMIT $n;",
	} {
		t.Run(sql, func(t *testing.T) {
			query := "-- name: Page :many\n" + sql
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			require.Equal(t, query, result.Queries[0].SQL)
		})
	}
}

func TestLimitOffsetPreservesInferredTypes(t *testing.T) {
	for _, tail := range []string{"WHERE id = $n LIMIT $n", "WHERE id = $n LIMIT 2 OFFSET $n", "WHERE id = $n LIMIT $n, 2"} {
		result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Page :many\nSELECT id FROM records " + tail + ";"}})
		require.NoError(t, err)
		{
			params := result.Queries[0].Parameters
			require.Equal(t, []model.Parameter{{Name: "n", Type: model.Type{Kind: "Uint32"}}}, params)
		}
	}
}

func TestLimitOffsetAcrossUnionArmsNeedsDeclare(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"}}
	statement := "SELECT id FROM records LIMIT $n UNION ALL SELECT id FROM records WHERE id = $n;"
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Page :many\n" + statement}})
	require.ErrorContains(t, err, "external parameter $n has incompatible inferred types; add DECLARE")

	sql := "-- name: Page :many\nDECLARE $n AS Uint32;\n" + statement
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	query := result.Queries[0]
	want := []model.Parameter{{Name: "n", Type: model.Type{Kind: "Uint32"}}}
	require.Equal(t, sql, query.SQL)
	require.True(t, query.IsDeclaredParameter("n"))
	require.Equal(t, want, query.Parameters)
}

func TestLimitOffsetDiagnostics(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"DECLARE $n AS Int64; SELECT id FROM records LIMIT $n;", "LIMIT expression has unsupported type Int64"},
		{"DECLARE $n AS BigInt?; SELECT id FROM records LIMIT 2 OFFSET $n;", "OFFSET expression has unsupported type Optional<Int64>"},
		{"DECLARE $n AS Utf8; SELECT id FROM records LIMIT $n, 2;", "OFFSET expression has unsupported type Utf8"},
		{"DECLARE $n AS Optional<Optional<Int32>>; SELECT id FROM records LIMIT $n;", "LIMIT expression has unsupported type Optional<Optional<Int32>>"},
		{"DECLARE $n AS List<Uint32>; SELECT id FROM records LIMIT $n;", "LIMIT expression has unsupported type List<Uint32>"},
		{"SELECT id FROM records WHERE label = $n LIMIT $n;", "LIMIT expression has unsupported type Optional<Utf8>"},
		{"SELECT id FROM records WHERE id = $n AND label = $n LIMIT $n;", "incompatible inferred types"},
		{"DECLARE $n AS Uint32; SELECT id FROM records LIMIT 0, $n / 2u;", "unsupported arithmetic operator"},
		{"SELECT id FROM records LIMIT CAST(1 AS Int64);", "LIMIT expression has unsupported type Int64"},
		{"$n = 1l; SELECT id FROM records LIMIT $n;", "LIMIT expression has unsupported type Int64"},
		{"SELECT id FROM records LIMIT 1.5;", "LIMIT expression has unsupported type Double"},
		{"SELECT id FROM records LIMIT TRUE;", "LIMIT expression has unsupported type Bool"},
		{"SELECT id FROM records LIMIT '2';", "LIMIT expression has unsupported type String"},
		{"SELECT id FROM records LIMIT id;", "cannot resolve LIMIT expression: unknown column"},
		{"SELECT id FROM records LIMIT COUNT(*);", "aggregate"},
		{"SELECT id FROM records LIMIT IF(TRUE, 1ul, COUNT(*));", "aggregate"},
		{"SELECT id FROM records LIMIT $n + 1u;", "cannot resolve type of parameter $n; add DECLARE"},
		{"SELECT id FROM records LIMIT ($n);", "cannot resolve type of parameter $n; add DECLARE"},
		{"SELECT id FROM records LIMIT -1;", "unsupported result expression"},
		{"SELECT id FROM records LIMIT 18446744073709551616ul;", "out of range for Uint64"},
		{"SELECT id FROM records LIMIT 9223372036854775808l;", "out of range for Int64"},
		{"SELECT id FROM records LIMIT Unsupported(2);", "unsupported YQL function"},
		{"SELECT id FROM records UNION ALL SELECT id FROM records LIMIT FALSE;", "LIMIT expression has unsupported type Bool"},
		{"SELECT id FROM records LIMIT $n UNION ALL SELECT id FROM records WHERE id = $n;", "incompatible inferred types"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			_, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint32 NOT NULL, label Utf8, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Page :many\n" + tc.sql}})
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestLimitOffsetInDMLSelect(t *testing.T) {
	for _, verb := range []string{"INSERT INTO", "UPSERT INTO", "UPDATE", "DELETE FROM"} {
		for _, typ := range []string{"Int", "Utf8"} {
			source := "SELECT id FROM records LIMIT $n"
			if verb == "UPDATE" || verb == "DELETE FROM" {
				source = "ON " + source
			}
			sql := "-- name: Write :exec\nDECLARE $n AS " + typ + "; " + verb + " records " + source + ";"
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: sql}})
			if typ == "Utf8" {
				require.ErrorContains(t, err, "LIMIT expression has unsupported type Utf8")
			} else {
				require.NoError(t, err)
				require.Equal(t, "Int32", result.Queries[0].Parameters[0].Type.String())
			}
		}
	}
}
