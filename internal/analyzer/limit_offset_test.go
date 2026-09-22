package analyzer

import (
	"reflect"
	"strings"
	"testing"

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
			if err != nil {
				t.Fatal(err)
			}
			query := result.Queries[0]
			if query.SQL != sql || len(query.Parameters) != 2 {
				t.Fatalf("unexpected query: %#v", query)
			}
			for _, param := range query.Parameters {
				if param.Type.String() != tc.want || !query.IsDeclaredParameter(param.Name) {
					t.Fatalf("parameter = %#v, want declared %s", param, tc.want)
				}
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
			if err != nil {
				t.Fatal(err)
			}
			query := result.Queries[0]
			if query.SQL != sql || query.Parameters[0].Type.String() != tc.want || query.ResultSets[0].Columns[0].Type.String() != tc.want || result.Catalog.Tables[0].Columns[0].Type.String() != tc.want {
				t.Fatalf("alias was not canonicalized consistently: %#v", result)
			}
			typ, err := parseType("List<Struct<value:" + tc.alias + "?>>")
			if err != nil || typ.String() != "List<Struct<`value`:Optional<"+tc.want+">>>" {
				t.Fatalf("nested alias = %s, %v", typ.String(), err)
			}
		})
	}
	for _, alias := range []string{"Uint", "Unsigned", "Long", "Short", "Byte"} {
		if _, err := parseType(alias); err == nil || !strings.Contains(err.Error(), "unsupported YQL type") {
			t.Fatalf("unsupported alias %s: %v", alias, err)
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
			if err != nil {
				t.Fatal(err)
			}
			if result.Queries[0].SQL != query {
				t.Fatalf("SQL changed: %s", result.Queries[0].SQL)
			}
		})
	}
}

func TestLimitOffsetPreservesInferredTypes(t *testing.T) {
	for _, tail := range []string{"WHERE id = $n LIMIT $n", "WHERE id = $n LIMIT 2 OFFSET $n", "WHERE id = $n LIMIT $n, 2"} {
		result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint32 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Page :many\nSELECT id FROM records " + tail + ";"}})
		if err != nil {
			t.Fatal(err)
		}
		if params := result.Queries[0].Parameters; !reflect.DeepEqual(params, []model.Parameter{{Name: "n", Type: model.Type{Kind: "Uint32"}}}) {
			t.Fatalf("parameters = %#v", params)
		}
	}
}

func TestLimitOffsetDiagnostics(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"DECLARE $n AS Int64; SELECT id FROM records LIMIT $n;", "LIMIT expression has unsupported type Int64"},
		{"DECLARE $n AS BigInt?; SELECT id FROM records LIMIT 2 OFFSET $n;", "OFFSET expression has unsupported type Optional<Int64>"},
		{"DECLARE $n AS Utf8; SELECT id FROM records LIMIT $n, 2;", "OFFSET expression has unsupported type Utf8"},
		{"DECLARE $n AS Optional<Optional<Int32>>; SELECT id FROM records LIMIT $n;", "LIMIT expression has unsupported type Optional<Optional<Int32>>"},
		{"DECLARE $n AS List<Uint32>; SELECT id FROM records LIMIT $n;", "LIMIT expression has unsupported type List<Uint32>"},
		{"SELECT id FROM records WHERE label = $n LIMIT $n;", "LIMIT expression has unsupported type Optional<Utf8>"},
		{"SELECT id FROM records WHERE id = $n AND label = $n LIMIT $n;", "incompatible column types"},
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
		{"SELECT id FROM records LIMIT $n UNION ALL SELECT id FROM records WHERE id = $n;", "incompatible column types"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			_, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint32 NOT NULL, label Utf8, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Page :many\n" + tc.sql}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
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
				if err == nil || !strings.Contains(err.Error(), "LIMIT expression has unsupported type Utf8") {
					t.Fatalf("%s: %v", sql, err)
				}
			} else if err != nil || result.Queries[0].Parameters[0].Type.String() != "Int32" {
				t.Fatalf("%s: %#v, %v", sql, result, err)
			}
		}
	}
}
