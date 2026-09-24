package analyzer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestRejectsInvalidDecimalTypesAtAnalysisBoundary(t *testing.T) {
	for _, typ := range []string{
		"Decimal(0,0)", "Decimal(36,0)", "Decimal(2,3)",
		"Decimal(999999999999999999999999999999,0)",
		"Decimal(10,999999999999999999999999999999)",
		"Optional<Decimal(0,0)>", "List<Decimal(2,3)>",
	} {
		t.Run(typ, func(t *testing.T) {
			for _, schema := range []bool{false, true} {
				t.Run(fmt.Sprintf("schema=%t", schema), func(t *testing.T) {
					var schemas, queries []model.Source
					if schema {
						schemas = []model.Source{{Name: "schema.sql", Text: "CREATE TABLE t (id Uint64 NOT NULL, amount " + typ + ", PRIMARY KEY (id));"}}
					} else {
						queries = []model.Source{{Name: "query.sql", Text: "-- name: Amount :one\nDECLARE $amount AS " + typ + ";\nSELECT $amount AS amount;"}}
					}
					_, err := Analyze(schemas, queries)
					require.ErrorContains(t, err, "Decimal")
				})
			}
		})
	}
}

func TestDecimalPrecisionAndScaleBoundaries(t *testing.T) {
	for _, typ := range []string{"Decimal(1,0)", "Decimal(35,0)", "Decimal(35,35)", "Optional<Decimal(35,35)>"} {
		t.Run(typ, func(t *testing.T) {
			result, err := Analyze(
				[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE t (id Uint64 NOT NULL, amount " + typ + " NOT NULL, PRIMARY KEY (id));"}},
				[]model.Source{{Name: "query.sql", Text: "-- name: Amount :one\nDECLARE $amount AS " + typ + ";\nSELECT $amount AS amount;"}},
			)
			require.NoError(t, err)
			{
				got := result.Catalog.Tables[0].Columns[1].Type.String()
				require.Equal(t, typ, got)
			}
			{
				got := result.Queries[0].Parameters[0].Type.String()
				require.Equal(t, typ, got)
			}
		})
	}
}

func TestDeclarationsPreserveSourceAndMetadata(t *testing.T) {
	for _, sql := range []string{
		"-- name: Greeting :one\n-- DECLARE in a comment\nDECLARE /* type */ $name\nAS Utf8; -- trailing comment\n$prefix = \"DECLARE $other AS Utf8; \"u;\nSELECT $name AS greeting;",
		"-- name: Answer :one\nSELECT 42 AS answer;",
		"-- name: Pair :one\nDECLARE $one AS Uint64;\r\nDECLARE $two AS Optional<Utf8>;\nSELECT $one AS one, $two AS two;",
	} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
		require.NoError(t, err)
		q := result.Queries[0]
		require.Equal(t, sql, q.SQL)
		require.False(t, q.IsDeclaredParameter("other"))
		require.False(t, strings.Contains(sql, "Greeting") && (!q.IsDeclaredParameter("name") || len(q.DeclaredParameters) != 1))
		require.False(t, strings.Contains(sql, "Answer") && len(q.DeclaredParameters) != 0)
		require.False(t, strings.Contains(sql, "Pair") && (!q.IsDeclaredParameter("one") || !q.IsDeclaredParameter("two") || len(q.DeclaredParameters) != 2))
	}
}

func TestRejectsUnknownYQLTypesAtAnalysisBoundary(t *testing.T) {
	for _, typ := range []string{"Mystery", "TzDate32", "TzDatetime64", "TzTimestamp64"} {
		contexts := []string{"column"}
		if typ == "Mystery" {
			contexts = append(contexts, "declaration", "cast")
		}
		for _, context := range contexts {
			t.Run(typ+"/"+context, func(t *testing.T) {
				var schema, queries []model.Source
				switch context {
				case "column":
					schema = []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, value " + typ + ", PRIMARY KEY (id));"}}
				case "declaration":
					queries = []model.Source{{Name: "query.sql", Text: "-- name: Value :one\nDECLARE $value AS " + typ + "; SELECT $value AS value;"}}
				case "cast":
					queries = []model.Source{{Name: "query.sql", Text: "-- name: Value :one\nSELECT CAST(1 AS " + typ + ") AS value;"}}
				}
				got, err := Analyze(schema, queries)
				require.Error(t, err)
				require.Contains(t, err.Error(), "unsupported YQL type")
				require.Contains(t, err.Error(), typ)
				require.NotNil(t, got)
				require.NotEqual(t, 0, len(got.Diagnostics))
				require.False(t, got.Diagnostics[0].Position.Line < 1)
			})
		}
	}
}

func TestDeclaredParameterMetadata(t *testing.T) {
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE items (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Put :exec\nDECLARE $id AS Uint64;\nUPSERT INTO items (id,name) VALUES ($id,$name);"}})
	require.NoError(t, err)
	q := result.Queries[0]
	require.Len(t, q.DeclaredParameters, 1)
	require.Equal(t, "id", q.DeclaredParameters[0])
	require.True(t, q.IsDeclaredParameter("id"))
	require.False(t, q.IsDeclaredParameter("name"))
	require.Len(t, q.Parameters, 2)
}
