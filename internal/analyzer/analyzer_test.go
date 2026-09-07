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
		{Name: "name", Type: model.Type{Kind: "Utf8"}, Table: "authors"},
		{Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"}), Table: "books"},
	}
	if !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
	}
	wantParams := []model.Parameter{{Name: "wanted_id", Type: model.Type{Kind: "Uint64"}}}
	if !reflect.DeepEqual(got.Queries[0].Parameters, wantParams) {
		t.Fatalf("parameters = %#v, want %#v", got.Queries[0].Parameters, wantParams)
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

func TestAnalyzeRejectsUnsupportedSchemaStatements(t *testing.T) {
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));
DROP TABLE authors;`}}, nil)
	if err == nil || result == nil || !strings.Contains(err.Error(), "only CREATE TABLE is currently supported") {
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
