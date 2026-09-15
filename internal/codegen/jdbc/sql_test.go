package jdbc

import (
	"fmt"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJDBCParameterOccurrencesPreserveSQLText(t *testing.T) {
	q := model.AnalyzedQuery{
		SQL:        "-- name: Check :one\n$local = 'Привет $value';\nSELECT `$value`, $local, $value, $other, $value; -- $value\n",
		Parameters: []model.Parameter{{Name: "other"}, {Name: "value"}},
	}
	sql, bindings := SQL(q)
	want := "$local = 'Привет $value';\nSELECT `$value`, $local, ?, ?, ?; -- $value\n"
	if sql != want || fmt.Sprint(bindings) != "[1 0 1]" {
		t.Fatalf("SQL=%q bindings=%v", sql, bindings)
	}
}

func TestDeclaredParametersPreserveSourceAndAddOnlyInferredTypes(t *testing.T) {
	source := "-- name: Check :exec\nDECLARE $z AS Uint64; -- explicit declaration\nSELECT $z, $a, $z;"
	q := model.AnalyzedQuery{SQL: source, DeclaredParameters: []string{"z"}, Parameters: []model.Parameter{{Name: "a", Type: model.Type{Kind: "Utf8"}}, {Name: "z", Type: model.Type{Kind: "Uint64"}}}}
	got, bindings := SQL(q)
	want := "DECLARE $a AS Utf8;\n" + model.WithoutQueryAnnotation(source)
	if got != want || len(bindings) != 0 {
		t.Fatalf("got %q bindings %v", got, bindings)
	}
}

func TestMixedDeclarationsQuoteInferredParameterName(t *testing.T) {
	q := model.AnalyzedQuery{SQL: "DECLARE $z AS Uint64;\nSELECT $z, $`a-b`;", DeclaredParameters: []string{"z"}, Parameters: []model.Parameter{{Name: "z", Type: model.Type{Kind: "Uint64"}}, {Name: "a-b", Type: model.Type{Kind: "Utf8"}}}}
	got, _ := SQL(q)
	if want := "DECLARE $`a-b` AS Utf8;\n" + q.SQL; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
