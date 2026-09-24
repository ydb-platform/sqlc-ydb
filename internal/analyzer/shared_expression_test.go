package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const sharedExpressionSchema = `CREATE TABLE records (
    id Uint64 NOT NULL,
    enabled Bool NOT NULL,
    optional_flag Bool,
    optional_id Uint64,
    counter Uint32,
    label String,
    PRIMARY KEY(id)
);`

func TestParenthesizedColumnRetainsIdentity(t *testing.T) {
	for _, expression := range []string{"(id)", "((r.id))"} {
		result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT " + expression + " FROM records AS r;"}})
		require.NoError(t, err)
		column := result.Queries[0].ResultSets[0].Columns[0]
		require.Equal(t, "id", column.Name)
		require.Equal(t, "records", column.Table)
		require.Equal(t, "Uint64", column.Type.Kind)
	}
}

func TestOrderByProjectionAliasesAreScoped(t *testing.T) {
	for _, statement := range []string{
		"SELECT id + 1ul AS next_id FROM records ORDER BY next_id;",
		"SELECT id + 1ul FROM records ORDER BY column0;",
		"SELECT id, id + 1ul AS next_id FROM records ORDER BY next_id, id;",
	} {
		_, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + statement}})
		require.NoError(t, err)
	}
	for _, statement := range []string{
		"SELECT id + 1ul AS next_id FROM records WHERE next_id > 0ul;",
		"SELECT id + 1ul AS next_id, next_id AS another FROM records;",
		"SELECT id + 1ul AS next_id FROM records ORDER BY records.next_id;",
	} {
		_, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + statement}})
		require.ErrorContains(t, err, "unknown column")
	}
}

func TestSharedBooleanExpressions(t *testing.T) {
	for _, tc := range []struct{ expression, typ string }{
		{"id > 0ul", "Bool"},
		{"optional_id > 0ul", "Optional<Bool>"},
		{"optional_id IS NULL", "Bool"},
		{"optional_id IS NOT NULL", "Bool"},
		{"enabled AND id > 0ul", "Bool"},
		{"enabled OR optional_flag", "Optional<Bool>"},
		{"enabled XOR optional_flag", "Optional<Bool>"},
		{"NOT enabled", "Bool"},
		{"NOT optional_flag", "Optional<Bool>"},
		{"NOT (enabled AND (optional_id > 0ul OR optional_flag))", "Optional<Bool>"},
		{"IF(enabled AND id > 0ul, true, optional_flag)", "Optional<Bool>"},
		{"COALESCE(optional_id > 0ul, false)", "Bool"},
		{"NULL AND enabled", "Optional<Bool>"},
		{"NULL OR false", "Optional<Bool>"},
		{"NULL XOR true", "Optional<Bool>"},
		{"NULL AND NULL", "Optional<Bool>"},
		{"NULL = NULL", "Optional<Bool>"},
		{"NULL IS DISTINCT FROM NULL", "Bool"},
		{"NULL IS NOT DISTINCT FROM NULL", "Bool"},
		{"COALESCE(NOT NULL, false)", "Bool"},
		{"COALESCE(NULL || 'x', 'fallback') = 'fallback'", "Bool"},
		{"optional_id IS DISTINCT FROM id", "Bool"},
		{"optional_id IS NOT DISTINCT FROM id", "Bool"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			sql := "-- name: Read :many\nSELECT " + tc.expression + " AS value FROM records;"
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: sql}})
			require.NoError(t, err)
			query := result.Queries[0]
			require.Equal(t, tc.typ, query.ResultSets[0].Columns[0].Type.String())
			require.Equal(t, sql, query.SQL)
		})
	}
}

func TestSharedBooleanBindingsAndDML(t *testing.T) {
	for _, statement := range []string{
		"SELECT $valid AS valid;",
		"UPDATE records SET enabled = $valid, optional_flag = NOT ($valid AND optional_flag) WHERE id = $id;",
		"INSERT INTO records(id, enabled, optional_flag) VALUES ($id, $valid, $valid AND $flag);",
	} {
		sql := "-- name: Read :exec\nDECLARE $id AS Uint64; DECLARE $flag AS Bool?; $valid = ($id > 0ul) AND NOT ($flag IS NULL); " + statement
		if strings.HasPrefix(statement, "SELECT") {
			sql = strings.Replace(sql, ":exec", ":one", 1)
		}
		result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: sql}})
		require.NoError(t, err)
		require.Equal(t, sql, result.Queries[0].SQL)
		require.Equal(t, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "flag", Type: model.Optional(model.Type{Kind: "Bool"})}}, result.Queries[0].Parameters)
	}
}

func TestSharedBooleanExpressionsRejectNonBooleans(t *testing.T) {
	for _, expression := range []string{"enabled AND id", "id OR optional_flag", "enabled XOR label", "NOT id", "NOT id = 1ul"} {
		_, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT " + expression + " AS value FROM records;"}})
		require.ErrorContains(t, err, "want Bool")
	}
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nDECLARE $flag AS Optional<Optional<Bool>>; SELECT $flag AND true AS value;"}})
	require.ErrorContains(t, err, "want Bool or Optional<Bool>")
}

func TestSharedConcatenationOperands(t *testing.T) {
	for _, statement := range []string{
		"SELECT label || $prefix || '%' AS value FROM records;",
		"SELECT id FROM records WHERE label LIKE ($prefix || '%');",
		"SELECT id FROM records WHERE ($prefix || label) = ('pre' || $prefix);",
		"UPDATE records SET label = COALESCE(label, '') || $prefix WHERE label LIKE $prefix || '%';",
	} {
		command := ":many"
		if strings.HasPrefix(statement, "UPDATE") {
			command = ":exec"
		}
		sql := "-- name: Read " + command + "\nDECLARE $prefix AS String; " + statement
		result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: sql}})
		require.NoError(t, err)
		require.Equal(t, sql, result.Queries[0].SQL)
		require.False(t, strings.HasPrefix(statement, "SELECT label") && result.Queries[0].ResultSets[0].Columns[0].Type.String() != "Optional<String>")
	}
}

func TestSharedEmptyStringLiterals(t *testing.T) {
	for _, literal := range []string{"''", `""`, "@@@@"} {
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Empty :one\nSELECT " + literal + " || 'value' AS value;"}})
		require.NoError(t, err)
		{
			typ := result.Queries[0].ResultSets[0].Columns[0].Type
			require.Equal(t, "String", typ.Kind)
		}
	}
}

func TestSharedConcatenationRejectsUnresolvedOperands(t *testing.T) {
	_, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT id FROM records WHERE label LIKE Unknown::Pattern() || '%';"}})
	require.ErrorContains(t, err, `unsupported YQL function "Unknown::Pattern"`)
}

func TestSharedNullPredicatesAndConcatenation(t *testing.T) {
	for _, predicate := range []string{"NULL", "NOT NULL", "enabled OR NULL", "NULL = NULL", "NULL IS DISTINCT FROM NULL"} {
		_, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT id FROM records WHERE " + predicate + ";"}})
		require.NoError(t, err)
	}
	for _, expression := range []string{"NULL || 'x'", "'x' || NULL", "NULL || NULL"} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT " + expression + " AS value;"}})
		require.ErrorContains(t, err, "unresolved Null type")
	}
}

func TestCountIfAggregateSemantics(t *testing.T) {
	for _, statement := range []string{
		"SELECT COUNT_IF(optional_flag) AS total FROM records;",
		"SELECT COUNT_IF(id > 0ul AND optional_flag) AS total FROM records;",
		"SELECT enabled, COUNT_IF(id > 0ul) AS total FROM records GROUP BY enabled HAVING COUNT_IF(optional_flag) > 0ul;",
	} {
		result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + statement}})
		require.NoError(t, err)
		columns := result.Queries[0].ResultSets[0].Columns
		require.Equal(t, "Uint64", columns[len(columns)-1].Type.String())
	}
	for _, tc := range []struct{ statement, message string }{
		{"SELECT id, COUNT_IF(enabled) AS total FROM records;", "must appear in GROUP BY"},
		{"SELECT COUNT_IF(COUNT(*) > 0ul) AS total FROM records;", "cannot contain another aggregate"},
		{"SELECT SUM(COUNT_IF(enabled)) AS total FROM records;", "cannot contain another aggregate"},
		{"UPDATE records SET id = COUNT_IF(enabled);", "aggregate functions are not allowed in DML values"},
	} {
		command := ":many"
		if strings.HasPrefix(tc.statement, "UPDATE") {
			command = ":exec"
		}
		_, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read " + command + "\n" + tc.statement}})
		require.ErrorContains(t, err, tc.message)
	}
}

func TestAggregateFunctionsRequireAggregationContext(t *testing.T) {
	for _, statement := range []string{
		"SELECT COUNT(*) AS value;",
		"SELECT COUNT(true) AS value;",
		"SELECT SUM(1) AS value;",
		"SELECT COUNT_IF(true) AS value;",
		"$value = COUNT_IF(true); SELECT $value AS value FROM records;",
		"SELECT id FROM records WHERE COUNT_IF(enabled) > 0ul;",
		"SELECT a.id FROM records AS a JOIN records AS b ON COUNT_IF(a.enabled) = b.id;",
	} {
		_, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + statement}})
		require.ErrorContains(t, err, "aggregate")
	}
}

func TestCoalesceUsesIntegerLiteralValues(t *testing.T) {
	for _, tc := range []struct{ expression, typ string }{
		{"COALESCE(counter, 0)", "Uint32"},
		{"COALESCE(counter, (0l))", "Uint32"},
		{"NVL(counter, 0x0ul)", "Uint32"},
		{"COALESCE(counter, 0o1l)", "Uint32"},
		{"COALESCE(counter, 0b1ul)", "Uint32"},
		{"COALESCE(counter, 18446744073709551615ul)", "Uint64"},
		{"COALESCE(counter, 0l, 1ul)", "Uint32"},
		{"COALESCE(0l, counter)", "Int64"},
		{"COALESCE(counter, $fallback)", "Int64"},
		{"COALESCE(counter, 1l + 1l)", "Int64"},
	} {
		result, err := Analyze([]model.Source{{Name: "schema.sql", Text: sharedExpressionSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nDECLARE $fallback AS Int64; SELECT " + tc.expression + " AS value FROM records;"}})
		require.NoError(t, err)
		{
			typ := result.Queries[0].ResultSets[0].Columns[0].Type
			require.Equal(t, tc.typ, typ.Kind)
		}
	}
}
