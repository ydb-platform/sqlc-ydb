package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeResolvesNestedCaseCastAndCoalesce(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    raw String NOT NULL,
    enabled Bool NOT NULL,
    label Utf8,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Display :many
SELECT
    CASE WHEN enabled THEN label ELSE "disabled"u END AS display,
    COALESCE(CAST(raw AS Uint64), 0ul) AS parsed
FROM records;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Column{
		{Name: "display", Type: model.Optional(model.Type{Kind: "Utf8"})},
		{Name: "parsed", Type: model.Type{Kind: "Uint64"}},
	}
	{
		columns := got.Queries[0].ResultSets[0].Columns
		require.Equal(t, want, columns)
	}
}

func TestAnalyzeResolvesOptionalOrdinaryFunctionResult(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    label Utf8,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Matches :many
SELECT StartsWith(label, "pre"u) AS matches FROM records;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := model.Optional(model.Type{Kind: "Bool"})
	{
		result := got.Queries[0].ResultSets[0].Columns[0]
		require.Equal(t, "matches", result.Name)
		require.Equal(t, want, result.Type)
	}
}

func TestAnalyzeResolvesQualifiedLibraryFunctions(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    payload String,
    label Utf8,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Encoded :many
SELECT
    String::Base64Encode(payload) AS encoded,
    Unicode::GetLength(label) AS length
FROM records;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Column{
		{Name: "encoded", Type: model.Optional(model.Type{Kind: "String"})},
		{Name: "length", Type: model.Optional(model.Type{Kind: "Uint64"})},
	}
	{
		columns := got.Queries[0].ResultSets[0].Columns
		require.Equal(t, want, columns)
	}
}

func TestAnalyzeRejectsIncompatibleCaseBranches(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: Invalid :one
SELECT CASE WHEN true THEN 1 ELSE "one"u END AS value;`}})
	require.ErrorContains(t, err, "CASE branches have incompatible types")
}

func TestAnalyzeResolvesCaseComparisonCondition(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Sign :many
SELECT CASE WHEN id > 0ul THEN "positive"u ELSE "zero"u END AS sign FROM records;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := model.Type{Kind: "Utf8"}
	{
		result := got.Queries[0].ResultSets[0].Columns[0]
		require.Equal(t, "sign", result.Name)
		require.Equal(t, want, result.Type)
	}
}

func TestAnalyzeResolvesSimpleCaseAndIfCondition(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Labels :many
SELECT
    CASE id WHEN 0ul THEN "zero"u ELSE "other"u END AS case_label,
    IF(id > 0ul, "positive"u, "zero"u) AS if_label
FROM records;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Column{
		{Name: "case_label", Type: model.Type{Kind: "Utf8"}},
		{Name: "if_label", Type: model.Type{Kind: "Utf8"}},
	}
	{
		columns := got.Queries[0].ResultSets[0].Columns
		require.Equal(t, want, columns)
	}
}

func TestAnalyzeResolvesTypedLocalExpression(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Parse :one
DECLARE $raw AS String;
$parsed = COALESCE(CAST($raw AS Uint64), 0ul);
SELECT $parsed AS parsed;`}}

	got, err := Analyze(nil, queries)
	require.NoError(t, err)
	wantParameters := []model.Parameter{{Name: "raw", Type: model.Type{Kind: "String"}}}
	{
		parameters := got.Queries[0].Parameters
		require.Equal(t, wantParameters, parameters)
	}
	wantResult := model.Type{Kind: "Uint64"}
	{
		result := got.Queries[0].ResultSets[0].Columns[0]
		require.Equal(t, "parsed", result.Name)
		require.Equal(t, wantResult, result.Type)
	}
}

func TestAnalyzeRejectsUnresolvedNullResult(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: NullOnly :one
SELECT NULL AS value;`}})
	require.ErrorContains(t, err, `result column "value" has unresolved Null type`)
}

func TestAnalyzeRejectsUngroupedProjectionAndHavingColumn(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    category Utf8 NOT NULL,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: InvalidGrouping :many
SELECT id, COUNT(*) AS total
FROM records
GROUP BY category
HAVING id > 0;`}}

	_, err := Analyze(schema, queries)
	require.Error(t, err)
	for _, want := range []string{`projection column "id" must appear in GROUP BY or an aggregate function`, `HAVING column "id" must appear in GROUP BY or an aggregate function`} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestAnalyzeAcceptsGroupedProjectionAndAggregateHaving(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    category Utf8 NOT NULL,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: CategoryCounts :many
SELECT category, COUNT(*) AS total
FROM records
GROUP BY category
HAVING COUNT(*) > 0ul;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Column{
		{Name: "category", Type: model.Type{Kind: "Utf8"}, Table: "records"},
		{Name: "total", Type: model.Type{Kind: "Uint64"}},
	}
	{
		columns := got.Queries[0].ResultSets[0].Columns
		require.Equal(t, want, columns)
	}
}

func TestAnalyzeUsesGroupingContextForAggregateNullability(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    category Utf8 NOT NULL,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Grouped :many
SELECT MIN(id) AS minimum, SUM(id) AS total, AVG(id) AS average
FROM records
GROUP BY category;
-- name: Global :one
SELECT MIN(id) AS minimum, SUM(id) AS total, AVG(id) AS average
FROM records;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	wantGrouped := []model.Type{{Kind: "Uint64"}, {Kind: "Uint64"}, {Kind: "Double"}}
	wantGlobal := []model.Type{
		model.Optional(model.Type{Kind: "Uint64"}),
		model.Optional(model.Type{Kind: "Uint64"}),
		model.Optional(model.Type{Kind: "Double"}),
	}
	for i, want := range wantGrouped {
		{
			gotType := got.Queries[0].ResultSets[0].Columns[i].Type
			assert.Equal(t, want, gotType)
		}
	}
	for i, want := range wantGlobal {
		{
			gotType := got.Queries[1].ResultSets[0].Columns[i].Type
			assert.Equal(t, want, gotType)
		}
	}
}

func TestAnalyzeGroupingDistinguishesSameNamedJoinColumns(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE left_records (id Uint64 NOT NULL, PRIMARY KEY (id));
CREATE TABLE right_records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT r.id AS right_id, COUNT(*) AS total
FROM left_records AS l JOIN right_records AS r ON l.id = r.id
GROUP BY l.id;`}}

	_, err := Analyze(schema, queries)
	require.ErrorContains(t, err, `projection column "r.id" must appear in GROUP BY or an aggregate function`)
}

func TestAnalyzeRejectsNonBooleanHaving(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT id FROM records GROUP BY id HAVING 1;`}}

	_, err := Analyze(schema, queries)
	require.ErrorContains(t, err, "HAVING expression has type Int32, want Bool")
}

func TestAnalyzeRejectsStarAlongsideAggregate(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT *, COUNT(*) AS total FROM records GROUP BY category;`}}

	_, err := Analyze(schema, queries)
	require.ErrorContains(t, err, "star projections are unsupported in grouped or aggregate queries")
}

func TestAnalyzeRejectsNestedAggregate(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT SUM(COUNT(*)) AS total FROM records GROUP BY category;`}}

	_, err := Analyze(schema, queries)
	require.ErrorContains(t, err, `aggregate function "SUM" cannot contain another aggregate`)
}

func TestAnalyzeRejectsUnknownHavingFunction(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT COUNT(*) AS total FROM records HAVING Mystery(id);`}}

	_, err := Analyze(schema, queries)
	require.ErrorContains(t, err, `cannot resolve HAVING expression: unsupported YQL function "Mystery"`)
}
