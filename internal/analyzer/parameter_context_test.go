package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeInfersLimitAndOffsetParametersAsUint64(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Page :many
SELECT id FROM records LIMIT $limit OFFSET $offset;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Parameter{
		{Name: "limit", Type: model.Type{Kind: "Uint64"}},
		{Name: "offset", Type: model.Type{Kind: "Uint64"}},
	}
	{
		parameters := got.Queries[0].Parameters
		require.Equal(t, want, parameters)
	}
}

func TestAnalyzeInfersParametersInDirectInList(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Selected :many
SELECT id FROM records WHERE id IN ($first, $second);`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Parameter{
		{Name: "first", Type: model.Type{Kind: "Uint64"}},
		{Name: "second", Type: model.Type{Kind: "Uint64"}},
	}
	{
		parameters := got.Queries[0].Parameters
		require.Equal(t, want, parameters)
	}
}

func TestAnalyzeUsesInferredComparisonParametersInTypedExpressions(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Labels :many
SELECT
    CASE WHEN id = $case_id THEN "case"u ELSE "other"u END AS case_label,
    IF(id = $if_id, "if"u, "other"u) AS if_label
FROM records
GROUP BY id
HAVING id = $having_id;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Parameter{
		{Name: "case_id", Type: model.Type{Kind: "Uint64"}},
		{Name: "if_id", Type: model.Type{Kind: "Uint64"}},
		{Name: "having_id", Type: model.Type{Kind: "Uint64"}},
	}
	{
		parameters := got.Queries[0].Parameters
		require.Equal(t, want, parameters)
	}
}

func TestAnalyzeRejectsConflictingInferredExpressionParametersAcrossUnionArms(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE ids (
    value Uint64 NOT NULL,
    PRIMARY KEY (value)
);
CREATE TABLE labels (
    value Utf8 NOT NULL,
    PRIMARY KEY (value)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Matches :many
SELECT CASE WHEN value = $wanted THEN 1 ELSE 0 END AS matched FROM ids
UNION ALL
SELECT CASE WHEN value = $wanted THEN 1 ELSE 0 END AS matched FROM labels;`}}

	_, err := Analyze(schema, queries)
	require.ErrorContains(t, err, "external parameter $wanted has incompatible inferred types")
}

func TestAnalyzeRepeatedParametersKeepFirstUseOrder(t *testing.T) {
	got, err := Analyze([]model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Rows :many
SELECT id FROM records WHERE ($id = id OR id = $id) AND label = $label OR label = $label;`}})
	require.NoError(t, err)
	want := []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}}
	{
		params := got.Queries[0].Parameters
		require.Equal(t, want, params)
	}
}

func TestAnalyzeParameterContextsRequireDeclare(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}}
	for _, tt := range []struct{ name, sql string }{
		{"between", "SELECT id FROM records WHERE id BETWEEN $low AND $high;"},
		{"pattern", "SELECT id FROM records WHERE label LIKE $pattern;"},
		{"function", "SELECT id FROM records WHERE String::AsciiToLower($value) = 'label';"},
		{"cast", "SELECT id FROM records WHERE CAST($allow AS Bool);"},
		{"order", "SELECT id FROM records ORDER BY $sort;"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Rows :many\n" + tt.sql}})
			require.Error(t, err)
			require.Contains(t, err.Error(), "cannot resolve type of external parameter")
			require.Contains(t, err.Error(), "add DECLARE")
		})
	}
}

func TestAnalyzeJoinParametersInOnAndWhere(t *testing.T) {
	got, err := Analyze([]model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Rows :many
SELECT a.id FROM records a JOIN records b ON b.id = $joined WHERE a.label = $label;`}})
	require.NoError(t, err)
	want := []model.Parameter{{Name: "joined", Type: model.Type{Kind: "Uint64"}}, {Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}}
	{
		params := got.Queries[0].Parameters
		require.Equal(t, want, params)
	}
}

func TestAnalyzeInfersParameterFromConcatenatedStringLiteral(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Greeting :one
SELECT "hello "u || $name AS greeting;`}}

	got, err := Analyze(nil, queries)
	require.NoError(t, err)
	want := []model.Parameter{{Name: "name", Type: model.Type{Kind: "Utf8"}}}
	{
		parameters := got.Queries[0].Parameters
		require.Equal(t, want, parameters)
	}
}

func TestINContainerAndScalarParameters(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"id IN $ids", "List<Uint64>"},
		{"id NOT IN $ids", "List<Uint64>"},
		{"id IN ($ids)", "Uint64"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			result, err := Analyze(
				[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}},
				[]model.Source{{Name: "queries.sql", Text: "-- name: Find :many\nSELECT id FROM records WHERE " + tc.sql + ";"}},
			)
			require.NoError(t, err)
			{
				got := result.Queries[0].Parameters[0].Type.String()
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestINListExplicitDeclaration(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}},
		[]model.Source{{Name: "queries.sql", Text: "-- name: Find :many\nDECLARE $ids AS List<Uint64>;\nSELECT id FROM records WHERE id IN $ids;"}},
	)
	require.NoError(t, err)
	require.Equal(t, "List<Uint64>", got.Queries[0].Parameters[0].Type.String())
}
