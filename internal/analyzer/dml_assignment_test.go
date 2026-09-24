package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
			require.Equal(t, tc.valid, (err == nil))
			require.False(t, err == nil && result.Queries[0].SQL != sql)
		}
	}
	for _, statement := range []string{
		"INSERT INTO counters (id, value, optional_value, enabled) SELECT id, small, maybe, true FROM source;",
		"UPSERT INTO counters (id, value, optional_value, enabled) SELECT id, small, maybe, true FROM source;",
		"UPDATE counters ON SELECT id, small AS value, maybe AS optional_value FROM source;",
	} {
		{
			_, err := Analyze(schema, []model.Source{{Name: "q.sql", Text: "-- name: Write :exec\n" + statement}})
			require.NoError(t, err)
		}
	}
}

func TestUnresolvedExpressionParameterSuggestsDeclaration(t *testing.T) {
	for _, expr := range []string{"$delta", "($delta)", "1 + $delta", "1 + ($delta)", "ABS($delta)"} {
		_, err := Analyze(nil, []model.Source{{Name: "q.sql", Text: "-- name: Read :one\nSELECT " + expr + " AS value;"}})
		require.ErrorContains(t, err, "cannot resolve type of parameter $delta; add DECLARE")
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
			require.Equal(t, tc.valid, (err == nil))
		}
	}
}
