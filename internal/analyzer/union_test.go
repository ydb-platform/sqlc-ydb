package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{
		{Name: "id", Type: model.Optional(model.Type{Kind: "Uint64"})},
		{Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})},
	}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
	}
}

func TestAnalyzeUnionUsesCommonNumericType(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Numbers :many
SELECT 1 AS value
UNION
SELECT 2l AS value;`}}

	got, err := Analyze(nil, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := model.Type{Kind: "Int64"}
	if result := got.Queries[0].ResultSets[0].Columns[0]; result.Name != "value" || !reflect.DeepEqual(result.Type, want) {
		t.Fatalf("result = %#v, want value %#v", result, want)
	}
}

func TestAnalyzeUnionPreservesFirstInputOrder(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Reordered :many
SELECT 1 AS id, 2 AS z, 3 AS a
UNION ALL
SELECT 4 AS id, 5 AS a, 6 AS z;`}}

	got, err := Analyze(nil, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	wantNames := []string{"id", "z", "a"}
	columns := got.Queries[0].ResultSets[0].Columns
	if len(columns) != len(wantNames) {
		t.Fatalf("columns = %#v", columns)
	}
	for i, name := range wantNames {
		if columns[i].Name != name {
			t.Fatalf("column %d = %q, want %q; columns = %#v", i, columns[i].Name, name, columns)
		}
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	wantNames := []string{"z", "y", "d", "b", "a"}
	columns := got.Queries[0].ResultSets[0].Columns
	if len(columns) != len(wantNames) {
		t.Fatalf("columns = %#v", columns)
	}
	for i, name := range wantNames {
		if columns[i].Name != name {
			t.Fatalf("column %d = %q, want %q; columns = %#v", i, columns[i].Name, name, columns)
		}
	}
}

func TestAnalyzeUnionRejectsDuplicateNamesWithinInput(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT 1 AS value, 2 AS value
UNION ALL
SELECT 3 AS value;`}})
	if err == nil || !strings.Contains(err.Error(), `UNION input has duplicate result column "value"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeUnionResolvesContextualNull(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Values :many
SELECT NULL AS value
UNION ALL
SELECT 1 AS value;`}}

	got, err := Analyze(nil, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := model.Optional(model.Type{Kind: "Int32"})
	if result := got.Queries[0].ResultSets[0].Columns[0]; result.Name != "value" || !reflect.DeepEqual(result.Type, want) {
		t.Fatalf("result = %#v, want value %#v", result, want)
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{
		{Name: "a_id", WireName: "a.id", Type: model.Optional(model.Type{Kind: "Uint64"})},
		{Name: "b_id", WireName: "b.id", Type: model.Optional(model.Type{Kind: "Uint64"})},
	}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{
		{Name: "a_id", WireName: "a.id", Type: model.Type{Kind: "Uint64"}},
		{Name: "b_id", WireName: "b.id", Type: model.Type{Kind: "Uint64"}},
	}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{{Name: "id", WireName: "a.id", Type: model.Type{Kind: "Uint64"}}}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
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
	if err == nil || !strings.Contains(err.Error(), `UNION result columns "a.id" and "a_id" produce duplicate logical name "a_id"; use explicit unique AS aliases`) {
		t.Fatalf("error = %v", err)
	}
}
