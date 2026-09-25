package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestTypedINResolvesExpressionContexts(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, tc := range []struct{ name, statement, wantType string }{
		{"having", "SELECT id FROM records GROUP BY id HAVING id IN $ids;", "Uint64"},
		{"projection", "SELECT id IN $ids AS selected FROM records;", "Bool"},
		{"not projection", "SELECT id NOT IN $ids AS selected FROM records;", "Bool"},
		{"case", "SELECT CASE WHEN id IN $ids THEN 1u ELSE 0u END AS selected FROM records;", "Uint32"},
		{"if", "SELECT IF(id IN $ids, 1u, 0u) AS selected FROM records;", "Uint32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := "-- name: Read :many\nDECLARE $ids AS List<Uint64>;\n" + tc.statement
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			require.Equal(t, query, result.Queries[0].SQL)
			require.Equal(t, tc.wantType, result.Queries[0].ResultSets[0].Columns[0].Type.String())
		})
	}
}

func TestTypedINResolvesLambdaReturnAndScalarList(t *testing.T) {
	query := `-- name: MatchCount :one
DECLARE $values AS List<String>;
$filtered = ListFilter($values, ($x) -> { RETURN $x != ""; });
SELECT ListLength(ListFilter($filtered, ($x) -> { RETURN $x IN $values; })) AS count;`
	result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, "Uint64", result.Queries[0].ResultSets[0].Columns[0].Type.String())
	require.Equal(t, []model.Parameter{{Name: "values", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "String"}}}}, result.Queries[0].Parameters)
}

func TestTypedINResolvesListFunctionInsideLambda(t *testing.T) {
	query := `-- name: RemoveTag :one
DECLARE $tag AS String;
DECLARE $document AS Json;
SELECT Yson::SerializeJson(Json::From(ListFilter(
    Yson::ConvertToStringList($document),
    ($item) -> ($item NOT IN AsList($tag))
))) AS tags;`
	result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, "Optional<Json>", result.Queries[0].ResultSets[0].Columns[0].Type.String())
}

func TestTypedINResolvesDMLValues(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, selected Bool NOT NULL, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		"UPDATE records SET selected = id IN $ids WHERE id = 1ul;",
		"UPSERT INTO records (id, selected) VALUES (1ul, 1ul IN $ids);",
	} {
		t.Run(statement, func(t *testing.T) {
			query := "-- name: Write :exec\nDECLARE $ids AS List<Uint64>;\n" + statement
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			require.Equal(t, query, result.Queries[0].SQL)
		})
	}
}

func TestTypedINRejectsSubqueryInProjection(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	query := "-- name: Read :many\nSELECT id IN (SELECT id FROM records) AS selected FROM records;"
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.ErrorContains(t, err, "IN subqueries are supported only in WHERE predicates")
}

func TestTypedINResolvesNullableAndScalarOperands(t *testing.T) {
	for _, tc := range []struct{ name, declaration, expression, wantType string }{
		{"nullable left", "DECLARE $ids AS List<Uint64>;", "CAST(NULL AS Uint64?) IN $ids", "Optional<Bool>"},
		{"nullable element", "DECLARE $ids AS List<Uint64?>;", "1ul IN $ids", "Optional<Bool>"},
		{"nullable list", "DECLARE $ids AS List<Uint64>?;", "1ul IN $ids", "Optional<Bool>"},
		{"null left", "DECLARE $ids AS List<Uint64>;", "NULL IN $ids", "Optional<Bool>"},
		{"scalar values", "DECLARE $id AS Uint64;", "1ul IN ($id, 2ul)", "Bool"},
		{"nullable scalar", "DECLARE $id AS Uint64?;", "1ul IN ($id, 2ul)", "Optional<Bool>"},
		{"list expression", "", "1ul IN AsList(1ul, 2ul)", "Bool"},
		{"list literal", "", "1ul IN [1ul, 2ul]", "Bool"},
		{"nullable list literal", "", "1ul IN [CAST(NULL AS Uint64?), 2ul]", "Optional<Bool>"},
		{"converted list", "DECLARE $document AS Yson;", `"a" IN Yson::ConvertToStringList($document)`, "Bool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := "-- name: Read :one\n" + tc.declaration + "\nSELECT " + tc.expression + " AS selected;"
			result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			require.Equal(t, tc.wantType, result.Queries[0].ResultSets[0].Columns[0].Type.String())
		})
	}
}

func TestTypedINListLiteralRemainsSupportedInPredicates(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		"SELECT id FROM records WHERE id IN [1ul, 2ul];",
		"SELECT a.id FROM records AS a JOIN records AS b ON a.id = b.id AND a.id IN [1ul, 2ul];",
	} {
		t.Run(statement, func(t *testing.T) {
			query := "-- name: Read :many\n" + statement
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			require.Equal(t, query, result.Queries[0].SQL)
		})
	}
}

func TestTypedINErrorsNameExpressionOperands(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		"SELECT id IN $values AS selected FROM records;",
		"SELECT CASE WHEN id IN $values THEN 1u ELSE 0u END AS selected FROM records;",
		"SELECT IF(id IN $values, 1u, 0u) AS selected FROM records;",
		"SELECT id FROM records GROUP BY id HAVING id IN $values;",
	} {
		t.Run(statement, func(t *testing.T) {
			query := "-- name: Read :many\nDECLARE $values AS List<Utf8>;\n" + statement
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.ErrorContains(t, err, "IN operands have incompatible types")
			require.NotContains(t, err.Error(), "predicate operands")
		})
	}
}

func TestTypedINRejectsInvalidExpressionOperands(t *testing.T) {
	for _, tc := range []struct{ declaration, expression, want string }{
		{"", "missing IN [1ul]", `cannot resolve IN operand "missing"`},
		{"", "1ul IN 2ul", "requires a List expression"},
		{"", "1ul IN AsList(1ul)[0]", `unsupported scalar expression "AsList(1ul)[0]"`},
		{"DECLARE $document AS Json;", `1ul IN JSON_VALUE($document, "$.id")`, `unsupported scalar expression "JSON_VALUE(`},
		{"DECLARE $id AS Uint64;", "$id IN $id", "requires a List parameter"},
		{"DECLARE $ids AS List<Uint64>;", "1ul IN ($ids)", "parenthesized List parameter"},
		{"DECLARE $values AS List<Utf8>;", "1ul IN $values", "IN operands have incompatible types"},
		{"", "1ul IN ($x) -> (AsList($x))", `lambda is not a valid IN operand "($x)->(AsList($x))"; provide a List expression`},
		{"DECLARE $id AS Uint64;", "1ul IN Unknown::List($id)", "unsupported YQL function"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			query := "-- name: Read :one\n" + tc.declaration + "\nSELECT " + tc.expression + " AS selected;"
			_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestTypedINRejectsMissingOperandBeforeResolution(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT 1ul IN AS selected;"}})
	require.ErrorContains(t, err, "query.sql:2:")
}

func TestTypedINRemainsSupportedInPredicateContexts(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		"SELECT id FROM records WHERE id IN $ids;",
		"SELECT a.id FROM records AS a JOIN records AS b ON a.id = b.id AND a.id IN $ids;",
	} {
		t.Run(statement, func(t *testing.T) {
			query := "-- name: Read :many\nDECLARE $ids AS List<Uint64>;\n" + statement
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			analyzed := result.Queries[0]
			require.Equal(t, query, analyzed.SQL)
			require.Len(t, analyzed.Parameters, 1)
			require.Equal(t, "ids", analyzed.Parameters[0].Name)
			require.Equal(t, "List<Uint64>", analyzed.Parameters[0].Type.String())
			columns := analyzed.ResultSets[0].Columns
			require.Len(t, columns, 1)
			require.Equal(t, "Uint64", columns[0].Type.String())
		})
	}
}
