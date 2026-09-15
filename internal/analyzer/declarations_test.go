package analyzer

import (
	"fmt"
	"strings"
	"testing"

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
					if err == nil || !strings.Contains(err.Error(), "Decimal") {
						t.Fatalf("Analyze accepted invalid type %s: %v", typ, err)
					}
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
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Catalog.Tables[0].Columns[1].Type.String(); got != typ {
				t.Fatalf("schema type = %s, want %s", got, typ)
			}
			if got := result.Queries[0].Parameters[0].Type.String(); got != typ {
				t.Fatalf("parameter type = %s, want %s", got, typ)
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
		if err != nil {
			t.Fatal(err)
		}
		q := result.Queries[0]
		if q.SQL != sql {
			t.Fatalf("SQL changed: %q", q.SQL)
		}
		if q.IsDeclaredParameter("other") {
			t.Fatal("literal mistaken for declaration")
		}
		if strings.Contains(sql, "Greeting") && (!q.IsDeclaredParameter("name") || len(q.DeclaredParameters) != 1) {
			t.Fatal(q.DeclaredParameters)
		}
		if strings.Contains(sql, "Answer") && len(q.DeclaredParameters) != 0 {
			t.Fatal(q.DeclaredParameters)
		}
		if strings.Contains(sql, "Pair") && (!q.IsDeclaredParameter("one") || !q.IsDeclaredParameter("two") || len(q.DeclaredParameters) != 2) {
			t.Fatal(q.DeclaredParameters)
		}
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
				if err == nil || !strings.Contains(err.Error(), "unsupported YQL type") || !strings.Contains(err.Error(), typ) {
					t.Fatalf("error = %v, want unsupported type %s", err, typ)
				}
				if got == nil || len(got.Diagnostics) == 0 || got.Diagnostics[0].Position.Line < 1 {
					t.Fatalf("missing source diagnostic: %#v", got)
				}
			})
		}
	}
}

func TestDeclaredParameterMetadata(t *testing.T) {
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE items (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Put :exec\nDECLARE $id AS Uint64;\nUPSERT INTO items (id,name) VALUES ($id,$name);"}})
	if err != nil {
		t.Fatal(err)
	}
	q := result.Queries[0]
	if len(q.DeclaredParameters) != 1 || q.DeclaredParameters[0] != "id" || !q.IsDeclaredParameter("id") || q.IsDeclaredParameter("name") {
		t.Fatalf("declared parameter metadata: %#v", q.DeclaredParameters)
	}
	if len(q.Parameters) != 2 {
		t.Fatalf("parameters: %#v", q.Parameters)
	}
}
