package analyzer

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestDMLAssignmentWideningAcrossForms(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: computedDMLSchema + `
 CREATE TABLE source (id Utf8 NOT NULL, small Uint32 NOT NULL, wide Uint64 NOT NULL, maybe Uint32, PRIMARY KEY(id));`}}
	for _, form := range []string{
		"INSERT INTO counters (id, value, optional_value, enabled) VALUES ('a'u, %s, 1u, true);",
		"UPSERT INTO counters (id, value, optional_value, enabled) SELECT 'a'u, %s, 1u, true;",
		"UPDATE counters SET value = %s;",
		"UPDATE counters ON SELECT 'a'u AS id, %s AS value;",
	} {
		for _, tc := range []struct {
			value string
			valid bool
		}{{"1u", true}, {"1ul", false}, {"CAST(NULL AS Uint32?)", false}} {
			sql := "-- name: Write :exec\n" + strings.Replace(form, "%s", tc.value, 1)
			result, err := Analyze(schema, []model.Source{{Name: "q.sql", Text: sql}})
			if (err == nil) != tc.valid {
				t.Fatalf("%s: %v", sql, err)
			}
			if err == nil && result.Queries[0].SQL != sql {
				t.Fatalf("SQL changed: %s", result.Queries[0].SQL)
			}
		}
	}
	for _, statement := range []string{
		"INSERT INTO counters (id, value, optional_value, enabled) SELECT id, small, maybe, true FROM source;",
		"UPSERT INTO counters (id, value, optional_value, enabled) SELECT id, small, maybe, true FROM source;",
		"UPDATE counters ON SELECT id, small AS value, maybe AS optional_value FROM source;",
	} {
		if _, err := Analyze(schema, []model.Source{{Name: "q.sql", Text: "-- name: Write :exec\n" + statement}}); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func TestUnresolvedExpressionParameterSuggestsDeclaration(t *testing.T) {
	for _, expr := range []string{"$delta", "($delta)", "1 + $delta", "1 + ($delta)", "ABS($delta)"} {
		_, err := Analyze(nil, []model.Source{{Name: "q.sql", Text: "-- name: Read :one\nSELECT " + expr + " AS value;"}})
		if err == nil || !strings.Contains(err.Error(), "cannot resolve type of parameter $delta; add DECLARE") {
			t.Fatalf("%s: %v", expr, err)
		}
	}
}

func TestDMLPrimaryKeyWidening(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	for _, verb := range []string{"UPDATE", "DELETE FROM"} {
		for _, tc := range []struct {
			typ   string
			valid bool
		}{{"Uint32", true}, {"Uint64", true}, {"Uint32?", false}, {"Int32", false}} {
			sql := "-- name: Write :exec\nDECLARE $key AS " + tc.typ + ";\n" + verb + " records ON SELECT $key AS id;"
			_, err := Analyze(schema, []model.Source{{Name: "q.sql", Text: sql}})
			if (err == nil) != tc.valid {
				t.Fatalf("%s: %v", sql, err)
			}
		}
	}
}
