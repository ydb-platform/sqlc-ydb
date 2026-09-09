package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func TestAnalyzeBuildsCatalogAndResolvesDeclaredSelect(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (
    id Uint64 NOT NULL,
    name Utf8 NOT NULL,
    biography Text,
    PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
DECLARE $author_id AS Uint64;
SELECT id, name, biography FROM authors WHERE id = $author_id;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	wantCatalog := model.Catalog{Tables: []model.Table{{
		Name: "authors",
		Columns: []model.Column{
			{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "authors"},
			{Name: "name", Type: model.Type{Kind: "Utf8"}, Table: "authors"},
			{Name: "biography", Type: model.Optional(model.Type{Kind: "Utf8"}), Table: "authors"},
		},
		PrimaryKey: []string{"id"},
	}}}
	if !reflect.DeepEqual(got.Catalog, wantCatalog) {
		t.Fatalf("Catalog = %#v, want %#v", got.Catalog, wantCatalog)
	}
	if len(got.Queries) != 1 {
		t.Fatalf("len(Queries) = %d, want 1", len(got.Queries))
	}
	q := got.Queries[0]
	if q.Name != "GetAuthor" || q.Command != model.One {
		t.Fatalf("query identity = %q %q, want GetAuthor :one", q.Name, q.Command)
	}
	if q.Source != (model.Position{File: "query.sql", Line: 1, Column: 1}) {
		t.Fatalf("Source = %#v", q.Source)
	}
	if !strings.Contains(q.SQL, "DECLARE $author_id AS Uint64;") {
		t.Fatalf("SQL lost declaration: %q", q.SQL)
	}
	wantParams := []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}}
	if !reflect.DeepEqual(q.Parameters, wantParams) {
		t.Fatalf("Parameters = %#v, want %#v", q.Parameters, wantParams)
	}
	wantResult := []model.ResultSet{{Columns: wantCatalog.Tables[0].Columns}}
	if !reflect.DeepEqual(q.ResultSets, wantResult) {
		t.Fatalf("ResultSets = %#v, want %#v", q.ResultSets, wantResult)
	}
}

func TestAnalyzeMultipleQueriesAndDMLParameterTypes(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (
 id Uint64 NOT NULL,
 name Utf8 NOT NULL,
 biography Utf8,
 PRIMARY KEY (id)
);`}}
	queries := []model.Source{{Name: "queries.sql", Text: `-- name: CreateAuthor :exec
INSERT INTO authors (id, name, biography) VALUES ($id, $name, $bio);

-- name: RenameAuthor :execrows
UPDATE authors SET name = $new_name WHERE id = $author_id;

-- name: DeleteAuthor :exec
DELETE FROM authors WHERE id = $author_id;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(got.Queries) != 3 {
		t.Fatalf("len(Queries) = %d, want 3", len(got.Queries))
	}
	want := [][]model.Parameter{
		{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Type{Kind: "Utf8"}}, {Name: "bio", Type: model.Optional(model.Type{Kind: "Utf8"})}},
		{{Name: "new_name", Type: model.Type{Kind: "Utf8"}}, {Name: "author_id", Type: model.Type{Kind: "Uint64"}}},
		{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}},
	}
	for i := range want {
		if !reflect.DeepEqual(got.Queries[i].Parameters, want[i]) {
			t.Fatalf("Queries[%d].Parameters = %#v, want %#v", i, got.Queries[i].Parameters, want[i])
		}
	}
}

func TestAnalyzeJoinAliasesAndOptionalSide(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));
CREATE TABLE books (id Uint64 NOT NULL, author_id Uint64 NOT NULL, title Utf8 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: AuthorsAndBooks :many
SELECT a.id AS author_id, a.name, b.title
FROM authors AS a LEFT JOIN books AS b ON b.author_id = a.id
WHERE a.id = $wanted_id;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	columns := got.Queries[0].ResultSets[0].Columns
	want := []model.Column{
		{Name: "author_id", Type: model.Type{Kind: "Uint64"}, Table: "authors"},
		{Name: "name", WireName: "a.name", Type: model.Type{Kind: "Utf8"}, Table: "authors"},
		{Name: "title", WireName: "b.title", Type: model.Optional(model.Type{Kind: "Utf8"}), Table: "books"},
	}
	if !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
	}
	wantParams := []model.Parameter{{Name: "wanted_id", Type: model.Type{Kind: "Uint64"}}}
	if !reflect.DeepEqual(got.Queries[0].Parameters, wantParams) {
		t.Fatalf("parameters = %#v, want %#v", got.Queries[0].Parameters, wantParams)
	}
}

func TestAnalyzeYDBResultWireNames(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `
CREATE TABLE left_table (id Uint64 NOT NULL, PRIMARY KEY (id));
CREATE TABLE right_table (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: SingleQualified :many
SELECT x.id FROM left_table AS x;
-- name: SingleQualifiedStar :many
SELECT x.* FROM left_table AS x;
-- name: JoinQualified :many
SELECT x.id FROM left_table AS x JOIN right_table AS y ON x.id = y.id;
-- name: JoinQualifiedStar :many
SELECT x.* FROM left_table AS x JOIN right_table AS y ON x.id = y.id;
-- name: JoinAliased :many
SELECT x.id AS result FROM left_table AS x JOIN right_table AS y ON x.id = y.id;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{
		{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
		{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
		{Name: "id", WireName: "x.id", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
		{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
		{Name: "result", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
	}
	if len(got.Queries) != len(want) {
		t.Fatalf("got %d queries, want %d", len(got.Queries), len(want))
	}
	for i := range want {
		column := got.Queries[i].ResultSets[0].Columns[0]
		if !reflect.DeepEqual(column, want[i]) {
			t.Errorf("query %s column = %#v, want %#v", got.Queries[i].Name, column, want[i])
		}
	}
}

func TestAnalyzeDoesNotExposeLocalBindingsAsParameters(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
DECLARE $author_id AS Uint64;
$local_id = $author_id;
SELECT id FROM authors WHERE id = $local_id;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}}
	if !reflect.DeepEqual(got.Queries[0].Parameters, want) {
		t.Fatalf("Parameters = %#v, want %#v", got.Queries[0].Parameters, want)
	}
}

func TestAnalyzeValidatesColumnsOutsideProjection(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
SELECT id FROM authors WHERE missing = $id;`}}

	result, err := Analyze(schema, queries)
	if err == nil {
		t.Fatal("Analyze() error = nil, want unknown-column error")
	}
	if result == nil || len(result.Diagnostics) == 0 || !strings.Contains(err.Error(), `query.sql:2:30: unknown column "missing"`) {
		t.Fatalf("error = %v; diagnostics = %#v", err, result.Diagnostics)
	}
}

func TestAnalyzeRejectsConflictingDeclare(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
DECLARE $wanted AS Uint64;
DECLARE $wanted AS Utf8;
SELECT id FROM authors WHERE id = $wanted;`}}

	_, err := Analyze(schema, queries)
	if err == nil || !strings.Contains(err.Error(), "conflicting DECLARE types Uint64 and Utf8") {
		t.Fatalf("error = %v", err)
	}
}

func TestQueryAnnotationsComeOnlyFromLexerCommentTokens(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
$text = @@ -- name: NotAQuery :many @@;
/* -- name: AlsoNotAQuery :exec */
SELECT id FROM authors WHERE id = $id;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(got.Queries) != 1 || got.Queries[0].Name != "GetAuthor" {
		t.Fatalf("Queries = %#v", got.Queries)
	}
}

func TestParseYQLRejectsBackslashEscapesInQuotedIdentifiers(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		line   int
		column int
	}{
		{name: "schema table", text: "CREATE TABLE `bad\\nname` (id Uint64, PRIMARY KEY (id));", line: 1, column: 14},
		{name: "select column", text: "SELECT `bad\\nname` FROM authors;", line: 1, column: 8},
		{name: "result alias", text: "SELECT id AS `bad\\nname` FROM authors;", line: 1, column: 14},
		{name: "bind name", text: "DECLARE $`bad\\nname` AS Uint64;", line: 1, column: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics := parseYQL("input.sql", tt.text, 2)
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v, want one", diagnostics)
			}
			got := diagnostics[0]
			if got.Position != (model.Position{File: "input.sql", Line: tt.line + 2, Column: tt.column}) {
				t.Fatalf("position = %#v", got.Position)
			}
			if !strings.Contains(got.Message, "backslash escapes in quoted identifiers are unsupported") {
				t.Fatalf("message = %q", got.Message)
			}
		})
	}
}

func TestAnalyzeRejectsUnsupportedSchemaStatements(t *testing.T) {
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));
UPSERT INTO authors (id) VALUES (1);`}}, nil)
	if err == nil || result == nil || !strings.Contains(err.Error(), "unsupported schema statement") {
		t.Fatalf("error = %v; result = %#v", err, result)
	}
}

func TestAnalyzeRejectsComputedExpressionInsteadOfGuessingType(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Matches :many
SELECT id = 1 AS matches FROM authors;`}},
	)
	if err == nil || !strings.Contains(err.Error(), `computed result expression "id=1" is not supported`) {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsCompositeParameterProjectionInsteadOfUsingBindType(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Matches :many
DECLARE $value AS Uint64;
SELECT $value = 1ul AS matches FROM authors;`}},
	)
	if err == nil || !strings.Contains(err.Error(), `unsupported result expression "$value=1ul"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeKeepsDirectParameterProjection(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Echo :many
DECLARE $value AS Uint64;
SELECT $value AS value FROM authors;`}},
	)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := model.Type{Kind: "Uint64"}
	if resultType := got.Queries[0].ResultSets[0].Columns[0].Type; !reflect.DeepEqual(resultType, want) {
		t.Fatalf("result type = %#v, want %#v", resultType, want)
	}
}

func TestAnalyzeUsesYQLLiteralTypes(t *testing.T) {
	tests := []struct {
		name string
		expr string
		kind string
	}{
		{name: "default int32", expr: "1", kind: "Int32"},
		{name: "expanded int64", expr: "2147483648", kind: "Int64"},
		{name: "explicit int64", expr: "1l", kind: "Int64"},
		{name: "explicit int16", expr: "1s", kind: "Int16"},
		{name: "explicit int8", expr: "1t", kind: "Int8"},
		{name: "explicit uint64", expr: "18446744073709551615ul", kind: "Uint64"},
		{name: "uppercase explicit uint64", expr: "1UL", kind: "Uint64"},
		{name: "explicit uint32", expr: "1u", kind: "Uint32"},
		{name: "explicit uint16", expr: "1us", kind: "Uint16"},
		{name: "explicit uint8", expr: "1ut", kind: "Uint8"},
		{name: "hex uint8", expr: "0xffut", kind: "Uint8"},
		{name: "uppercase hex prefix", expr: "0Xffut", kind: "Uint8"},
		{name: "default double", expr: "1.5", kind: "Double"},
		{name: "explicit float", expr: "1.5f", kind: "Float"},
		{name: "default string", expr: `"hello"`, kind: "String"},
		{name: "explicit string", expr: `"hello"s`, kind: "String"},
		{name: "utf8", expr: `"hello"u`, kind: "Utf8"},
		{name: "uppercase utf8 suffix", expr: `"hello"U`, kind: "Utf8"},
		{name: "yson", expr: `"[]"y`, kind: "Yson"},
		{name: "json", expr: `"{}"j`, kind: "Json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(
				[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
				[]model.Source{{Name: "query.sql", Text: "-- name: Literal :many\nSELECT " + tt.expr + " AS value FROM authors;"}},
			)
			if err != nil {
				t.Fatalf("Analyze() error = %v", err)
			}
			if kind := got.Queries[0].ResultSets[0].Columns[0].Type.Kind; kind != tt.kind {
				t.Fatalf("literal %s type = %s, want %s", tt.expr, kind, tt.kind)
			}
		})
	}
}

func TestAnalyzeRejectsUnaryNumericLiteralUntilItsResultTypeIsSupported(t *testing.T) {
	for _, expr := range []string{"-1", "+1ul", "-128t"} {
		_, err := Analyze(
			[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
			[]model.Source{{Name: "query.sql", Text: "-- name: Literal :many\nSELECT " + expr + " AS value FROM authors;"}},
		)
		if err == nil || !strings.Contains(err.Error(), "unsupported result expression") {
			t.Errorf("literal %s error = %v", expr, err)
		}
	}
}

func TestAnalyzeRejectsIntegerLiteralOutsideItsYQLTypeRange(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Literal :many\nSELECT 256ut AS value FROM authors;"}},
	)
	if err == nil || !strings.Contains(err.Error(), `integer literal "256ut" is out of range for Uint8`) {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsLiteralSuffixesWithoutDocumentedModelTypes(t *testing.T) {
	for _, expr := range []string{"1p", `"value"p`} {
		_, err := Analyze(
			[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
			[]model.Source{{Name: "query.sql", Text: "-- name: Literal :many\nSELECT " + expr + " AS value FROM authors;"}},
		)
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("literal %s error = %v", expr, err)
		}
	}
}

func TestAnalyzeUsesLiteralTypeForLocalBinding(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Lookup :many
$value = 1ul;
SELECT id FROM authors WHERE id = $value;`}},
	)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
}

func TestAnalyzeRejectsExplainQuery(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Explained :many\nEXPLAIN SELECT id FROM authors;"}},
	)
	if err == nil || !strings.Contains(err.Error(), "EXPLAIN is unsupported in named queries") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeTreatsYQLIdentifiersAsCaseSensitive(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{name: "table", query: "SELECT ID FROM authors;", want: `unknown table "authors"`},
		{name: "column", query: "SELECT id FROM Authors;", want: `unknown column "id"`},
		{name: "alias", query: "SELECT a.ID FROM Authors AS A;", want: `unknown column "a.ID"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze(
				[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE Authors (ID Uint64 NOT NULL, PRIMARY KEY (ID));`}},
				[]model.Source{{Name: "query.sql", Text: "-- name: Lookup :many\n" + tt.query}},
			)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAnalyzeTreatsYQLBindNamesAsCaseSensitive(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE Authors (ID Uint64 NOT NULL, PRIMARY KEY (ID));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Lookup :many
DECLARE $Value AS Utf8;
SELECT ID FROM Authors WHERE ID = $value;`}},
	)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Parameter{
		{Name: "Value", Type: model.Type{Kind: "Utf8"}},
		{Name: "value", Type: model.Type{Kind: "Uint64"}},
	}
	if !reflect.DeepEqual(got.Queries[0].Parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", got.Queries[0].Parameters, want)
	}
}

func TestAnalyzeRejectsCastUntilItsNullabilityCanBeProven(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: IDs :many
SELECT CAST(id AS Utf8) AS id_text FROM authors;`}},
	)
	if err == nil || !strings.Contains(err.Error(), "CAST result nullability is not yet supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeResolvesCountFunction(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: CountAuthors :one
SELECT COUNT(*) AS count FROM authors;`}},
	)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Column{{Name: "count", Type: model.Type{Kind: "Uint64"}}}
	if !reflect.DeepEqual(got.Queries[0].ResultSets[0].Columns, want) {
		t.Fatalf("columns = %#v, want %#v", got.Queries[0].ResultSets[0].Columns, want)
	}
}

func TestAnalyzeRejectsParameterConstrainedByDifferentColumnTypes(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: FindAuthor :many
SELECT id FROM authors WHERE id = $value OR name = $value;`}},
	)
	if err == nil || !strings.Contains(err.Error(), "constrained by incompatible column types") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsOptionalParameterForRequiredDMLColumn(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: CreateAuthor :exec
DECLARE $id AS Optional<Uint64>;
INSERT INTO authors (id) VALUES ($id);`}},
	)
	if err == nil || !strings.Contains(err.Error(), "declared as Optional<Uint64> but used with Uint64") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeUpsertReturningSelectedColumns(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: PutAuthor :one
UPSERT INTO authors (id, name) VALUES ($id, $name) RETURNING id;`}},
	)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	wantColumns := []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "authors"}}
	if !reflect.DeepEqual(got.Queries[0].ResultSets[0].Columns, wantColumns) {
		t.Fatalf("columns = %#v, want %#v", got.Queries[0].ResultSets[0].Columns, wantColumns)
	}
}

func TestAnalyzeRejectsQueryPreamble(t *testing.T) {
	result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `DECLARE $id AS Uint64;
-- name: GetAuthor :one
SELECT $id AS id;`}})
	if err == nil || result == nil || !strings.Contains(err.Error(), "query file preamble") {
		t.Fatalf("error = %v; result = %#v", err, result)
	}
}

func TestAnalyzeAllowsCommentsBeforeQueries(t *testing.T) {
	query := "-- name: GetID :one\nSELECT 1 AS id;"
	result, err := Analyze(nil, []model.Source{
		{Name: "comments.sql", Text: "-- License header\n/* Комментарий */\n"},
		{Name: "query.sql", Text: "-- License header\n/* Комментарий */\n\n" + query},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Queries) != 1 || result.Queries[0].Name != "GetID" || result.Queries[0].SQL != query {
		t.Fatalf("queries = %#v", result.Queries)
	}
}

func TestAnalyzeReportsSyntaxPosition(t *testing.T) {
	result, err := Analyze(nil, []model.Source{{Name: "broken.sql", Text: `-- name: Broken :many
SELECT FROM;`}})
	if err == nil || result == nil || len(result.Diagnostics) == 0 {
		t.Fatalf("error = %v; result = %#v", err, result)
	}
	position := result.Diagnostics[0].Position
	if position.File != "broken.sql" || position.Line != 2 || position.Column < 1 {
		t.Fatalf("position = %#v", position)
	}
}

func TestAnalyzeRejectsDuplicateQueryNamesAcrossFiles(t *testing.T) {
	result, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{
			{Name: "one.sql", Text: "-- name: GetAuthor :one\nSELECT id FROM authors;"},
			{Name: "two.sql", Text: "-- name: GetAuthor :many\nSELECT id FROM authors;"},
		},
	)
	if err == nil || result == nil || !strings.Contains(err.Error(), `query "GetAuthor" is declared more than once`) {
		t.Fatalf("error = %v; result = %#v", err, result)
	}
}

func TestAnalyzeRejectsMixedUnsupportedQueryStatement(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: GetAuthor :one\nPRAGMA TablePathPrefix('/Root');\nSELECT id FROM authors;"}},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported statement in named query") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsFlattenSourceInsteadOfIgnoringItsTypeEffect(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, tags List<Utf8>, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Tags :many\nSELECT tags FROM authors FLATTEN LIST BY tags;"}},
	)
	if err == nil || !strings.Contains(err.Error(), "FLATTEN sources are not yet supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeChecksLocalBindingTypeAtUse(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
$local_id = "wrong type";
SELECT id FROM authors WHERE id = $local_id;`}},
	)
	if err == nil || !strings.Contains(err.Error(), "local $local_id has type String but is used with Uint64") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsCountComparisonInsteadOfCallingItCount(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: HasAuthors :one\nSELECT COUNT(*) > 0 AS has_authors FROM authors;"}},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported result expression") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeDoesNotInferScalarTypeForINParameter(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: FindAuthors :many\nSELECT id FROM authors WHERE id IN $ids;"}},
	)
	if err == nil || !strings.Contains(err.Error(), "cannot resolve type of external parameter $ids; add DECLARE") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeResolvesTableAliasWithoutAS(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: GetAuthor :one\nSELECT a.id FROM authors a WHERE a.id = $id;"}},
	)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if got.Queries[0].ResultSets[0].Columns[0].Name != "id" {
		t.Fatalf("query = %#v", got.Queries[0])
	}
}

func TestAnalyzeReportsUnknownProjectionOnce(t *testing.T) {
	result, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Broken :many\nSELECT missing FROM authors;"}},
	)
	if err == nil || result == nil {
		t.Fatalf("error = %v; result = %#v", err, result)
	}
	count := 0
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic.Message, `unknown column "missing"`) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unknown-column diagnostics = %d, want 1: %#v", count, result.Diagnostics)
	}
}

func TestAnalyzeRejectsDMLLiteralWithoutGuessingAssignmentType(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Broken :exec\nUPSERT INTO authors (id, name) VALUES ('wrong', 123);"}},
	)
	if err == nil || !strings.Contains(err.Error(), "DML values must be direct external parameters") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeRejectsLocalConstrainedByDifferentColumnTypes(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Broken :many
DECLARE $seed AS Uint64;
$local = $seed;
SELECT id FROM authors WHERE id = $local OR name = $local;`}},
	)
	if err == nil || !strings.Contains(err.Error(), "local $local is constrained by incompatible column types") {
		t.Fatalf("error = %v", err)
	}
}
