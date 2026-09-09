package analyzer

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

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
