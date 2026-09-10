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

func TestDeclarationFreeSQLPreservesNonDeclarationSource(t *testing.T) {
	const sql = `-- name: Greeting :one
-- Привет: DECLARE in a comment
DECLARE /* тип */ $name
AS Utf8; -- trailing comment
$prefix = "DECLARE $other AS Utf8; "u;
SELECT $name AS greeting;`
	const want = `-- name: Greeting :one
-- Привет: DECLARE in a comment` + "\n /* тип */ \n" + `  -- trailing comment
$prefix = "DECLARE $other AS Utf8; "u;
SELECT $name AS greeting;`
	result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	query := result.Queries[0]
	if query.SQL != sql || query.SQLWithoutDeclarations != want {
		t.Fatalf("original = %q\nwithout declarations = %q\nwant = %q", query.SQL, query.SQLWithoutDeclarations, want)
	}
	// Parsing the SDK's reconstructed query checks that removing tokens does not
	// splice comments or statements into a different YQL program.
	_, diagnostics := parseYQL("query.sql", "DECLARE $name AS Utf8;\n"+query.SQLWithoutDeclarations, 0)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
}

func TestDeclarationFreeSQLWithoutDeclarations(t *testing.T) {
	const sql = "-- name: Answer :one\nSELECT 42 AS answer;"
	result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Queries[0].SQLWithoutDeclarations != sql {
		t.Fatal("source without declarations changed")
	}
}

func TestDeclarationFreeSQLMultipleDeclarations(t *testing.T) {
	const sql = "DECLARE $one AS Uint64;\r\nDECLARE $two AS Optional<Utf8>;\nSELECT $one AS one, $two AS two;"
	parsed, diagnostics := parseYQL("query.sql", sql, 0)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	got := withoutDeclarations(sql, parsed.tokens, collectQueryTree(parsed.tree).declares)
	if strings.Contains(got, "DECLARE") || !strings.HasSuffix(got, "SELECT $one AS one, $two AS two;") || !strings.Contains(got, "\r\n") {
		t.Fatalf("unexpected declaration-free SQL: %q", got)
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
