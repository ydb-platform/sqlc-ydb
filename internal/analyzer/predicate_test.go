package analyzer

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestPredicateValidationCoversJoinAndDMLWhere(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));`}}
	for _, test := range []struct {
		name, query, want string
	}{
		{"join on", "-- name: Read :many\nSELECT a.id FROM records a JOIN records b ON a.id = Unknown::Hash(b.id);", "unsupported YQL function"},
		{"update where", "-- name: Write :exec\nDECLARE $id AS Uint64; UPDATE records SET label = $label WHERE id = Unknown::Hash($id);", "unsupported YQL function"},
		{"delete where", "-- name: Write :exec\nDECLARE $id AS Uint64; DELETE FROM records WHERE id = Unknown::Hash($id);", "unsupported YQL function"},
		{"incompatible", "-- name: Read :many\nSELECT id FROM records WHERE id = label;", "incompatible types"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: test.query}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPredicateValidationAcceptsBooleanCompositionAndTypedIN(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));`}}
	for _, predicate := range []string{
		"(id IN $ids OR id IS NULL) AND NOT (label IS NULL)",
		`StartsWith(label, "x"u)`,
		"ABS(id) > 1u",
		"IF(id = 1u, true, false)",
		"CASE WHEN id = 1u THEN true ELSE false END",
	} {
		query := "-- name: Read :many\nDECLARE $ids AS List<Uint64>;\nSELECT id FROM records WHERE " + predicate + ";"
		if _, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}}); err != nil {
			t.Fatalf("%s: %v", predicate, err)
		}
	}
}

func TestPredicateINValidatesItsActualOperands(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, test := range []struct{ name, declaration, predicate, want string }{
		{"unknown call", "DECLARE $id AS Uint64;", "id IN (Unknown::Hash($id))", "unsupported YQL function"},
		{"missing member", "DECLARE $key AS Struct<id:Uint64>;", "id IN ($key.missing)", "unknown struct field"},
		{"scalar direct", "DECLARE $id AS Uint64;", "id IN $id", "requires a List parameter"},
		{"parenthesized list", "DECLARE $ids AS List<Uint64>;", "id IN ($ids)", "incompatible types"},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := "-- name: Read :many\n" + test.declaration + " SELECT id FROM records WHERE " + test.predicate + ";"
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
