package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeUnionReconcilesColumnsByName(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE first_records (
    id Uint64 NOT NULL,
    title Utf8,
    PRIMARY KEY (id)
);
CREATE TABLE second_records (
    title Utf8 NOT NULL,
    PRIMARY KEY (title)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Combined :many
SELECT id AS id, title AS title FROM first_records
UNION ALL
SELECT title AS title FROM second_records;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Column{
		{Name: "id", Type: model.Optional(model.Type{Kind: "Uint64"})},
		{Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})},
	}
	{
		columns := got.Queries[0].ResultSets[0].Columns
		require.Equal(t, want, columns)
	}
}

func TestAnalyzeUnionUsesCommonNumericType(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Numbers :many
SELECT 1 AS value
UNION
SELECT 2l AS value;`}}

	got, err := Analyze(nil, queries)
	require.NoError(t, err)
	want := model.Type{Kind: "Int64"}
	{
		result := got.Queries[0].ResultSets[0].Columns[0]
		require.Equal(t, "value", result.Name)
		require.Equal(t, want, result.Type)
	}
}

func TestAnalyzeUnionPreservesFirstInputOrder(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Reordered :many
SELECT 1 AS id, 2 AS z, 3 AS a
UNION ALL
SELECT 4 AS id, 5 AS a, 6 AS z;`}}

	got, err := Analyze(nil, queries)
	require.NoError(t, err)
	wantNames := []string{"id", "z", "a"}
	columns := got.Queries[0].ResultSets[0].Columns
	require.Len(t, columns, len(wantNames))
	for i, name := range wantNames {
		require.Equal(t, name, columns[i].Name)
	}
}

func TestAnalyzeUnionAppendsNewColumnsInEncounterOrder(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Reordered :many
SELECT 1 AS z
UNION ALL
SELECT 2 AS y, 3 AS d
UNION ALL
SELECT 4 AS b, 5 AS a;`}}

	got, err := Analyze(nil, queries)
	require.NoError(t, err)
	wantNames := []string{"z", "y", "d", "b", "a"}
	columns := got.Queries[0].ResultSets[0].Columns
	require.Len(t, columns, len(wantNames))
	for i, name := range wantNames {
		require.Equal(t, name, columns[i].Name)
	}
}

func TestAnalyzeUnionRejectsDuplicateNamesWithinInput(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT 1 AS value, 2 AS value
UNION ALL
SELECT 3 AS value;`}})
	require.ErrorContains(t, err, `UNION input has duplicate result column "value"`)
}

func TestAnalyzeUnionResolvesContextualNull(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Values :many
SELECT NULL AS value
UNION ALL
SELECT 1 AS value;`}}

	got, err := Analyze(nil, queries)
	require.NoError(t, err)
	want := model.Optional(model.Type{Kind: "Int32"})
	{
		result := got.Queries[0].ResultSets[0].Columns[0]
		require.Equal(t, "value", result.Name)
		require.Equal(t, want, result.Type)
	}
}

func TestAnalyzeUnionReconcilesQualifiedJoinResultKeys(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Qualified :many
SELECT a.id FROM records AS a JOIN records AS b ON a.id = b.id
UNION ALL
SELECT b.id FROM records AS a JOIN records AS b ON a.id = b.id;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Column{
		{Name: "a_id", WireName: "a.id", Type: model.Optional(model.Type{Kind: "Uint64"})},
		{Name: "b_id", WireName: "b.id", Type: model.Optional(model.Type{Kind: "Uint64"})},
	}
	{
		columns := got.Queries[0].ResultSets[0].Columns
		require.Equal(t, want, columns)
	}
}

func TestAnalyzeUnionAllowsDistinctQualifiedKeysWithSameColumnName(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: QualifiedPair :many
SELECT a.id, b.id FROM records AS a JOIN records AS b ON a.id = b.id
UNION ALL
SELECT a.id, b.id FROM records AS a JOIN records AS b ON a.id = b.id;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Column{
		{Name: "a_id", WireName: "a.id", Type: model.Type{Kind: "Uint64"}},
		{Name: "b_id", WireName: "b.id", Type: model.Type{Kind: "Uint64"}},
	}
	{
		columns := got.Queries[0].ResultSets[0].Columns
		require.Equal(t, want, columns)
	}
}

func TestAnalyzeUnionKeepsLogicalNameForOneQualifiedResultKey(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Qualified :many
SELECT a.id FROM records AS a JOIN records AS b ON a.id = b.id
UNION ALL
SELECT a.id FROM records AS a JOIN records AS b ON a.id = b.id;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Column{{Name: "id", WireName: "a.id", Type: model.Type{Kind: "Uint64"}}}
	{
		columns := got.Queries[0].ResultSets[0].Columns
		require.Equal(t, want, columns)
	}
}

func TestAnalyzeUnionRejectsLogicalNameCollisionAfterQualifyingResults(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Uint64 NOT NULL,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Colliding :many
SELECT a.id, b.id, 1 AS a_id FROM records AS a JOIN records AS b ON a.id = b.id
UNION ALL
SELECT a.id, b.id, 2 AS a_id FROM records AS a JOIN records AS b ON a.id = b.id;`}}

	_, err := Analyze(schema, queries)
	require.ErrorContains(t, err, `UNION result columns "a.id" and "a_id" produce duplicate logical name "a_id"; use explicit unique AS aliases`)
}

func TestAnalyzeUnionPaginationParameters(t *testing.T) {
	got, err := Analyze([]model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Page :many
(SELECT id FROM records)
UNION ALL
SELECT id FROM records ORDER BY id LIMIT $limit OFFSET $offset;`}})
	require.NoError(t, err)
	want := []model.Parameter{{Name: "limit", Type: model.Type{Kind: "Uint64"}}, {Name: "offset", Type: model.Type{Kind: "Uint64"}}}
	q := got.Queries[0]
	require.Equal(t, want, q.Parameters)
	{
		columns := q.ResultSets[0].Columns
		require.Len(t, columns, 1)
		require.Equal(t, "id", columns[0].Name)
		require.True(t, columns[0].Type.Equal(model.Type{Kind: "Uint64"}))
	}
}
