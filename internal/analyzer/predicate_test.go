package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestPredicateValidationAcceptsBooleanCompositionAndTypedIN(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));`}}
	for _, predicate := range []string{
		"(id IN $ids OR id IS NULL) AND NOT (label IS NULL)",
		"id BETWEEN 1u AND 10u",
		`label LIKE "x%"u`,
		`StartsWith(label, "x"u)`,
		"ABS(id) > 1u",
		"IF(id = 1u, true, false)",
		"CASE WHEN id = 1u THEN true ELSE false END",
	} {
		query := "-- name: Read :many\nDECLARE $ids AS List<Uint64>;\nSELECT id FROM records WHERE " + predicate + ";"
		{
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
		}
	}
}

func TestPredicateOperatorsRejectIncompatibleOperands(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));`}}
	for _, predicate := range []string{
		"NOT id = 1u",
		`id BETWEEN "a"u AND "z"u`,
		"label LIKE 1u",
		"1u + (id = 1u)",
		"-(id = 1u)",
	} {
		query := "-- name: Read :many\nSELECT id FROM records WHERE " + predicate + ";"
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
		require.Error(t, err)
		require.False(t, (!strings.Contains(err.Error(), "incompatible types") && !strings.Contains(err.Error(), "NOT operand has type") && !strings.Contains(err.Error(), "unsupported scalar expression") && !strings.Contains(err.Error(), "requires primitive numeric operands")))
	}
}

func TestNotPrecedenceMatchesYDB(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, flag Bool NOT NULL, other Bool NOT NULL, PRIMARY KEY(id));`}}
	for _, predicate := range []string{"NOT flag = other", "NOT (id = 1u)"} {
		query := "-- name: Read :many\nSELECT id FROM records WHERE " + predicate + ";"
		{
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
		}
	}
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT id FROM records WHERE NOT id = 1u;"}})
	require.ErrorContains(t, err, "NOT operand has type Uint64")
}

func TestPredicateINValidatesItsActualOperands(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, test := range []struct{ name, declaration, predicate, want string }{
		{"unknown call", "DECLARE $id AS Uint64;", "id IN (Unknown::Hash($id))", "unsupported YQL function"},
		{"missing member", "DECLARE $key AS Struct<id:Uint64>;", "id IN ($key.missing)", "unknown struct field"},
		{"scalar direct", "DECLARE $id AS Uint64;", "id IN $id", "requires a List parameter"},
		{"parenthesized list", "DECLARE $ids AS List<Uint64>;", "id IN ($ids)", "parenthesized List parameter"},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := "-- name: Read :many\n" + test.declaration + " SELECT id FROM records WHERE " + test.predicate + ";"
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestPredicateRejectsBareAndNegatedNonBooleanValues(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, flag Bool NOT NULL, PRIMARY KEY(id));`}}
	for _, test := range []struct{ predicate, want string }{
		{"id", "predicate expression has type Uint64, want Bool"},
		{"label", "predicate expression has type Optional<Utf8>, want Bool"},
		{"NOT id", "NOT operand has type Uint64, want Bool"},
		{"NOT label", "NOT operand has type Optional<Utf8>, want Bool"},
		{"flag AND id", "predicate expression has type Uint64, want Bool"},
	} {
		t.Run(test.predicate, func(t *testing.T) {
			query := "-- name: Read :many\nSELECT id FROM records WHERE " + test.predicate + ";"
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.ErrorContains(t, err, "invalid predicate: "+test.want)
		})
	}
}

func TestPredicateINRejectsIncompatibleAndUnsupportedCollections(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, test := range []struct{ declaration, predicate, want string }{
		{`DECLARE $labels AS List<Utf8>;`, "id IN $labels", "IN operands have incompatible types"},
		{"", `id IN ("text"u)`, "IN operands have incompatible types"},
		{"", "id IN []", `unsupported IN operand "[]"`},
	} {
		t.Run(test.predicate, func(t *testing.T) {
			query := "-- name: Read :many\n" + test.declaration + " SELECT id FROM records WHERE " + test.predicate + ";"
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.ErrorContains(t, err, "invalid predicate: "+test.want)
		})
	}
}

func TestPredicateComparisonFamiliesUseResolvedOperands(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));`}}
	for _, predicate := range []string{
		`label LIKE "a%"u`,
		`label NOT LIKE "a%"u`,
		"id BETWEEN 1u AND 10u",
		"id NOT BETWEEN 1u AND 10u",
		"id = 1u",
		"label IS DISTINCT FROM NULL",
		"label IS NOT DISTINCT FROM NULL",
	} {
		t.Run(predicate, func(t *testing.T) {
			query := "-- name: Read :many\nSELECT id FROM records WHERE " + predicate + ";"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			analyzed := result.Queries[0]
			columns := analyzed.ResultSets[0].Columns
			require.Equal(t, query, analyzed.SQL)
			require.Len(t, columns, 1)
			require.Equal(t, "id", columns[0].Name)
			require.Equal(t, "Uint64", columns[0].Type.String())
		})
	}
}
