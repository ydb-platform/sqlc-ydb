package analyzer

import (
	"reflect"
	"strings"
	"testing"

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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{
		{Name: "display", Type: model.Optional(model.Type{Kind: "Utf8"})},
		{Name: "parsed", Type: model.Type{Kind: "Uint64"}},
	}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := model.Optional(model.Type{Kind: "Bool"})
	if result := got.Queries[0].ResultSets[0].Columns[0]; result.Name != "matches" || !reflect.DeepEqual(result.Type, want) {
		t.Fatalf("result = %#v, want matches %#v", result, want)
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{
		{Name: "encoded", Type: model.Optional(model.Type{Kind: "String"})},
		{Name: "length", Type: model.Optional(model.Type{Kind: "Uint64"})},
	}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
	}
}

func TestAnalyzeRejectsIncompatibleCaseBranches(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: Invalid :one
SELECT CASE WHEN true THEN 1 ELSE "one"u END AS value;`}})
	if err == nil || !strings.Contains(err.Error(), "CASE branches have incompatible types") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeResolvesCaseComparisonCondition(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Sign :many
SELECT CASE WHEN id > 0ul THEN "positive"u ELSE "zero"u END AS sign FROM records;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := model.Type{Kind: "Utf8"}
	if result := got.Queries[0].ResultSets[0].Columns[0]; result.Name != "sign" || !reflect.DeepEqual(result.Type, want) {
		t.Fatalf("result = %#v, want sign %#v", result, want)
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{
		{Name: "case_label", Type: model.Type{Kind: "Utf8"}},
		{Name: "if_label", Type: model.Type{Kind: "Utf8"}},
	}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
	}
}

func TestAnalyzeResolvesTypedLocalExpression(t *testing.T) {
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Parse :one
DECLARE $raw AS String;
$parsed = COALESCE(CAST($raw AS Uint64), 0ul);
SELECT $parsed AS parsed;`}}

	got, err := Analyze(nil, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	wantParameters := []model.Parameter{{Name: "raw", Type: model.Type{Kind: "String"}}}
	if parameters := got.Queries[0].Parameters; !reflect.DeepEqual(parameters, wantParameters) {
		t.Fatalf("parameters = %#v, want %#v", parameters, wantParameters)
	}
	wantResult := model.Type{Kind: "Uint64"}
	if result := got.Queries[0].ResultSets[0].Columns[0]; result.Name != "parsed" || !reflect.DeepEqual(result.Type, wantResult) {
		t.Fatalf("result = %#v, want parsed %#v", result, wantResult)
	}
}

func TestAnalyzeRejectsUnresolvedNullResult(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: NullOnly :one
SELECT NULL AS value;`}})
	if err == nil || !strings.Contains(err.Error(), `result column "value" has unresolved Null type`) {
		t.Fatalf("error = %v", err)
	}
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
	if err == nil {
		t.Fatal("Analyze() error = nil, want grouping diagnostics")
	}
	for _, want := range []string{`projection column "id" must appear in GROUP BY or an aggregate function`, `HAVING column "id" must appear in GROUP BY or an aggregate function`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q", err, want)
		}
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{
		{Name: "category", Type: model.Type{Kind: "Utf8"}, Table: "records"},
		{Name: "total", Type: model.Type{Kind: "Uint64"}},
	}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
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
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	wantGrouped := []model.Type{{Kind: "Uint64"}, {Kind: "Uint64"}, {Kind: "Double"}}
	wantGlobal := []model.Type{
		model.Optional(model.Type{Kind: "Uint64"}),
		model.Optional(model.Type{Kind: "Uint64"}),
		model.Optional(model.Type{Kind: "Double"}),
	}
	for i, want := range wantGrouped {
		if gotType := got.Queries[0].ResultSets[0].Columns[i].Type; !reflect.DeepEqual(gotType, want) {
			t.Errorf("grouped column %d type = %#v, want %#v", i, gotType, want)
		}
	}
	for i, want := range wantGlobal {
		if gotType := got.Queries[1].ResultSets[0].Columns[i].Type; !reflect.DeepEqual(gotType, want) {
			t.Errorf("global column %d type = %#v, want %#v", i, gotType, want)
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
	if err == nil || !strings.Contains(err.Error(), `projection column "r.id" must appear in GROUP BY or an aggregate function`) {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsNonBooleanHaving(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT id FROM records GROUP BY id HAVING 1;`}}

	_, err := Analyze(schema, queries)
	if err == nil || !strings.Contains(err.Error(), "HAVING expression has type Int32, want Bool") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsStarAlongsideAggregate(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT *, COUNT(*) AS total FROM records GROUP BY category;`}}

	_, err := Analyze(schema, queries)
	if err == nil || !strings.Contains(err.Error(), "star projections are unsupported in grouped or aggregate queries") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsNestedAggregate(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT SUM(COUNT(*)) AS total FROM records GROUP BY category;`}}

	_, err := Analyze(schema, queries)
	if err == nil || !strings.Contains(err.Error(), `aggregate function "SUM" cannot contain another aggregate`) {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsUnknownHavingFunction(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Invalid :many
SELECT COUNT(*) AS total FROM records HAVING Mystery(id);`}}

	_, err := Analyze(schema, queries)
	if err == nil || !strings.Contains(err.Error(), `cannot resolve HAVING expression: unsupported YQL function "Mystery"`) {
		t.Fatalf("error = %v", err)
	}
}
