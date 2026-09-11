package jdbc

import (
	"fmt"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJDBCParameterOccurrencesPreserveSQLText(t *testing.T) {
	q := model.AnalyzedQuery{
		SQLWithoutDeclarations: "-- name: Check :one\n$local = 'Привет $value';\nSELECT `$value`, $local, $value, $other, $value; -- $value\n",
		Parameters:             []model.Parameter{{Name: "other"}, {Name: "value"}},
	}
	sql, bindings := SQL(q)
	want := "$local = 'Привет $value';\nSELECT `$value`, $local, ?, ?, ?; -- $value\n"
	if sql != want || fmt.Sprint(bindings) != "[1 0 1]" {
		t.Fatalf("SQL=%q bindings=%v", sql, bindings)
	}
}
