package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func TestAnalyzeInfersLimitAndOffsetParametersAsUint64(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Page :many
SELECT id FROM records LIMIT $limit OFFSET $offset;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Parameter{
		{Name: "limit", Type: model.Type{Kind: "Uint64"}},
		{Name: "offset", Type: model.Type{Kind: "Uint64"}},
	}
	if parameters := got.Queries[0].Parameters; !reflect.DeepEqual(parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", parameters, want)
	}
}

func TestAnalyzeInfersParametersInDirectInList(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Selected :many
SELECT id FROM records WHERE id IN ($first, $second);`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Parameter{
		{Name: "first", Type: model.Type{Kind: "Uint64"}},
		{Name: "second", Type: model.Type{Kind: "Uint64"}},
	}
	if parameters := got.Queries[0].Parameters; !reflect.DeepEqual(parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", parameters, want)
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Parameter{
		{Name: "case_id", Type: model.Type{Kind: "Uint64"}},
		{Name: "if_id", Type: model.Type{Kind: "Uint64"}},
		{Name: "having_id", Type: model.Type{Kind: "Uint64"}},
	}
	if parameters := got.Queries[0].Parameters; !reflect.DeepEqual(parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", parameters, want)
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
	if err == nil || !strings.Contains(err.Error(), "external parameter $wanted is constrained by incompatible column types") {
		t.Fatalf("error = %v", err)
	}
}
