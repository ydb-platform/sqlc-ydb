package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
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
	require.NoError(t, err)

	wantCatalog := model.Catalog{Tables: []model.Table{{
		Name: "authors",
		Columns: []model.Column{
			{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "authors"},
			{Name: "name", Type: model.Type{Kind: "Utf8"}, Table: "authors"},
			{Name: "biography", Type: model.Optional(model.Type{Kind: "Utf8"}), Table: "authors"},
		},
		PrimaryKey: []string{"id"},
	}}}
	require.Equal(t, wantCatalog, got.Catalog)
	require.Len(t, got.Queries, 1)
	q := got.Queries[0]
	require.Equal(t, "GetAuthor", q.Name)
	require.Equal(t, model.One, q.Command)
	require.Equal(t, (model.Position{File: "query.sql", Line: 1, Column: 1}), q.Source)
	require.Contains(t, q.SQL, "DECLARE $author_id AS Uint64;")
	wantParams := []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}}
	require.Equal(t, wantParams, q.Parameters)
	wantResult := []model.ResultSet{{Columns: wantCatalog.Tables[0].Columns}}
	require.Equal(t, wantResult, q.ResultSets)
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
	require.NoError(t, err)
	require.Len(t, got.Queries, 3)
	want := [][]model.Parameter{
		{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Type{Kind: "Utf8"}}, {Name: "bio", Type: model.Optional(model.Type{Kind: "Utf8"})}},
		{{Name: "new_name", Type: model.Type{Kind: "Utf8"}}, {Name: "author_id", Type: model.Type{Kind: "Uint64"}}},
		{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}},
	}
	for i := range want {
		require.Equal(t, want[i], got.Queries[i].Parameters)
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
	require.NoError(t, err)
	columns := got.Queries[0].ResultSets[0].Columns
	want := []model.Column{
		{Name: "author_id", Type: model.Type{Kind: "Uint64"}, Table: "authors"},
		{Name: "name", WireName: "a.name", Type: model.Type{Kind: "Utf8"}, Table: "authors"},
		{Name: "title", WireName: "b.title", Type: model.Optional(model.Type{Kind: "Utf8"}), Table: "books"},
	}
	require.Equal(t, want, columns)
	wantParams := []model.Parameter{{Name: "wanted_id", Type: model.Type{Kind: "Uint64"}}}
	require.Equal(t, wantParams, got.Queries[0].Parameters)
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
	require.NoError(t, err)
	want := []model.Column{
		{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
		{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
		{Name: "id", WireName: "x.id", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
		{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
		{Name: "result", Type: model.Type{Kind: "Uint64"}, Table: "left_table"},
	}
	require.Len(t, got.Queries, len(want))
	for i := range want {
		column := got.Queries[i].ResultSets[0].Columns[0]
		assert.Equal(t, want[i], column)
	}
}

func TestAnalyzeDoesNotExposeLocalBindingsAsParameters(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
DECLARE $author_id AS Uint64;
$local_id = $author_id;
SELECT id FROM authors WHERE id = $local_id;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	want := []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}}
	require.Equal(t, want, got.Queries[0].Parameters)
}

func TestAnalyzeValidatesColumnsOutsideProjection(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
SELECT id FROM authors WHERE missing = $id;`}}

	result, err := Analyze(schema, queries)
	require.Error(t, err)
	require.NotNil(t, result)
	require.NotEqual(t, 0, len(result.Diagnostics))
	require.Contains(t, err.Error(), `query.sql:2:30: unknown column "missing"`)
}

func TestAnalyzeRejectsConflictingDeclare(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
DECLARE $wanted AS Uint64;
DECLARE $wanted AS Utf8;
SELECT id FROM authors WHERE id = $wanted;`}}

	_, err := Analyze(schema, queries)
	require.ErrorContains(t, err, "conflicting DECLARE types Uint64 and Utf8")
}

func TestQueryAnnotationsComeOnlyFromLexerCommentTokens(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
$text = @@ -- name: NotAQuery :many @@;
/* -- name: AlsoNotAQuery :exec */
SELECT id FROM authors WHERE id = $id;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	require.Len(t, got.Queries, 1)
	require.Equal(t, "GetAuthor", got.Queries[0].Name)
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
			require.Len(t, diagnostics, 1)
			got := diagnostics[0]
			require.Equal(t, (model.Position{File: "input.sql", Line: tt.line + 2, Column: tt.column}), got.Position)
			require.Contains(t, got.Message, "backslash escapes in quoted identifiers are unsupported")
		})
	}
}

func TestAnalyzeRejectsUnsupportedSchemaStatements(t *testing.T) {
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));
UPSERT INTO authors (id) VALUES (1);`}}, nil)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Contains(t, err.Error(), "unsupported schema statement")
}

func TestAnalyzeResolvesComparisonProjectionAsBool(t *testing.T) {
	result, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Matches :many
SELECT id = 1 AS matches FROM authors;`}},
	)
	require.NoError(t, err)
	{
		typ := result.Queries[0].ResultSets[0].Columns[0].Type
		require.Equal(t, "Bool", typ.Kind)
	}
}

func TestAnalyzeComparisonProjectionDiffersFromParameterType(t *testing.T) {
	result, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Matches :many
DECLARE $value AS Uint64;
SELECT $value = 1ul AS matches FROM authors;`}},
	)
	require.NoError(t, err)
	{
		typ := result.Queries[0].ResultSets[0].Columns[0].Type
		require.Equal(t, "Bool", typ.Kind)
	}
}

func TestAnalyzeKeepsDirectParameterProjection(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Echo :many
DECLARE $value AS Uint64;
SELECT $value AS value FROM authors;`}},
	)
	require.NoError(t, err)
	want := model.Type{Kind: "Uint64"}
	{
		resultType := got.Queries[0].ResultSets[0].Columns[0].Type
		require.Equal(t, want, resultType)
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
			require.NoError(t, err)
			{
				kind := got.Queries[0].ResultSets[0].Columns[0].Type.Kind
				require.Equal(t, tt.kind, kind)
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
		assert.ErrorContains(t, err, "unsupported result expression")
	}
}

func TestAnalyzeRejectsIntegerLiteralOutsideItsYQLTypeRange(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Literal :many\nSELECT 256ut AS value FROM authors;"}},
	)
	require.ErrorContains(t, err, `integer literal "256ut" is out of range for Uint8`)
}

func TestAnalyzeRejectsLiteralSuffixesWithoutDocumentedModelTypes(t *testing.T) {
	for _, expr := range []string{"1p", `"value"p`} {
		_, err := Analyze(
			[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
			[]model.Source{{Name: "query.sql", Text: "-- name: Literal :many\nSELECT " + expr + " AS value FROM authors;"}},
		)
		assert.ErrorContains(t, err, "unsupported")
	}
}

func TestAnalyzeUsesLiteralTypeForLocalBinding(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Lookup :many
$value = 1ul;
SELECT id FROM authors WHERE id = $value;`}},
	)
	require.NoError(t, err)
}

func TestAnalyzeRejectsExplainQuery(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Explained :many\nEXPLAIN SELECT id FROM authors;"}},
	)
	require.ErrorContains(t, err, "EXPLAIN is unsupported in named queries")
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
			require.ErrorContains(t, err, tt.want)
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
	require.NoError(t, err)
	want := []model.Parameter{
		{Name: "Value", Type: model.Type{Kind: "Utf8"}},
		{Name: "value", Type: model.Type{Kind: "Uint64"}},
	}
	require.Equal(t, want, got.Queries[0].Parameters)
}

func TestAnalyzeResolvesVerifiedCastNullability(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: IDs :many
SELECT CAST(id AS String) AS id_text FROM authors;`}},
	)
	require.NoError(t, err)
	want := model.Type{Kind: "String"}
	{
		result := got.Queries[0].ResultSets[0].Columns[0]
		require.Equal(t, "id_text", result.Name)
		require.Equal(t, want, result.Type)
	}
}

func TestAnalyzeResolvesCountFunction(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: CountAuthors :one
SELECT COUNT(*) AS count FROM authors;`}},
	)
	require.NoError(t, err)
	want := []model.Column{{Name: "count", Type: model.Type{Kind: "Uint64"}}}
	require.Equal(t, want, got.Queries[0].ResultSets[0].Columns)
}

func TestAnalyzeRejectsParameterConstrainedByDifferentColumnTypes(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: FindAuthor :many
SELECT id FROM authors WHERE id = $value OR name = $value;`}},
	)
	require.ErrorContains(t, err, "incompatible inferred types")
}

func TestAnalyzeRejectsOptionalParameterForRequiredDMLColumn(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: CreateAuthor :exec
DECLARE $id AS Optional<Uint64>;
INSERT INTO authors (id) VALUES ($id);`}},
	)
	require.ErrorContains(t, err, "declared as Optional<Uint64> but used with Uint64")
}

func TestAnalyzeUpsertReturningSelectedColumns(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: PutAuthor :one
UPSERT INTO authors (id, name) VALUES ($id, $name) RETURNING id;`}},
	)
	require.NoError(t, err)
	wantColumns := []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "authors"}}
	require.Equal(t, wantColumns, got.Queries[0].ResultSets[0].Columns)
}

func TestAnalyzeRejectsQueryPreamble(t *testing.T) {
	result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `DECLARE $id AS Uint64;
-- name: GetAuthor :one
SELECT $id AS id;`}})
	require.Error(t, err)
	require.NotNil(t, result)
	require.Contains(t, err.Error(), "query file preamble")
}

func TestAnalyzeAllowsCommentsBeforeQueries(t *testing.T) {
	query := "-- name: GetID :one\nSELECT 1 AS id;"
	result, err := Analyze(nil, []model.Source{
		{Name: "comments.sql", Text: "-- License header\n/* Комментарий */\n"},
		{Name: "query.sql", Text: "-- License header\n/* Комментарий */\n\n" + query},
	})
	require.NoError(t, err)
	require.Len(t, result.Queries, 1)
	require.Equal(t, "GetID", result.Queries[0].Name)
	require.Equal(t, query, result.Queries[0].SQL)
}

func TestAnalyzeReportsSyntaxPosition(t *testing.T) {
	result, err := Analyze(nil, []model.Source{{Name: "broken.sql", Text: `-- name: Broken :many
SELECT FROM;`}})
	require.Error(t, err)
	require.NotNil(t, result)
	require.NotEqual(t, 0, len(result.Diagnostics))
	position := result.Diagnostics[0].Position
	require.Equal(t, "broken.sql", position.File)
	require.Equal(t, 2, position.Line)
	require.False(t, position.Column < 1)
}

func TestAnalyzeRejectsDuplicateQueryNamesAcrossFiles(t *testing.T) {
	result, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{
			{Name: "one.sql", Text: "-- name: GetAuthor :one\nSELECT id FROM authors;"},
			{Name: "two.sql", Text: "-- name: GetAuthor :many\nSELECT id FROM authors;"},
		},
	)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Contains(t, err.Error(), `query "GetAuthor" is declared more than once`)
}

func TestAnalyzeRejectsMixedUnsupportedQueryStatement(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: GetAuthor :one\nPRAGMA AnsiInForEmptyOrNullableItemsCollections;\nSELECT id FROM authors;"}},
	)
	require.ErrorContains(t, err, "unsupported PRAGMA")
}

func TestAnalyzeRejectsFlattenSourceInsteadOfIgnoringItsTypeEffect(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, tags List<Utf8>, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Tags :many\nSELECT tags FROM authors FLATTEN LIST BY tags;"}},
	)
	require.ErrorContains(t, err, "FLATTEN sources are not yet supported")
}

func TestAnalyzeChecksLocalBindingTypeAtUse(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: GetAuthor :one
$local_id = "wrong type";
SELECT id FROM authors WHERE id = $local_id;`}},
	)
	require.ErrorContains(t, err, "local $local_id has type String but is used with Uint64")
}

func TestAnalyzeCountComparisonReturnsBool(t *testing.T) {
	result, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: HasAuthors :one\nSELECT COUNT(*) > 0 AS has_authors FROM authors;"}},
	)
	require.NoError(t, err)
	{
		typ := result.Queries[0].ResultSets[0].Columns[0].Type
		require.Equal(t, "Bool", typ.Kind)
	}
}

func TestAnalyzeInfersListTypeForINParameter(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: FindAuthors :many\nSELECT id FROM authors WHERE id IN $ids;"}},
	)
	require.NoError(t, err)
	require.Equal(t, "List<Uint64>", got.Queries[0].Parameters[0].Type.String())
}

func TestAnalyzeResolvesTableAliasWithoutAS(t *testing.T) {
	got, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: GetAuthor :one\nSELECT a.id FROM authors a WHERE a.id = $id;"}},
	)
	require.NoError(t, err)
	require.Equal(t, "id", got.Queries[0].ResultSets[0].Columns[0].Name)
}

func TestAnalyzeReportsUnknownProjectionOnce(t *testing.T) {
	result, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Broken :many\nSELECT missing FROM authors;"}},
	)
	require.Error(t, err)
	require.NotNil(t, result)
	count := 0
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic.Message, `unknown column "missing"`) {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestAnalyzeRejectsDMLLiteralWithoutGuessingAssignmentType(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Broken :exec\nUPSERT INTO authors (id, name) VALUES ('wrong', 123);"}},
	)
	require.ErrorContains(t, err, "cannot assign String to column \"id\" of type Uint64")
}

func TestAnalyzeRejectsLocalConstrainedByDifferentColumnTypes(t *testing.T) {
	_, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));`}},
		[]model.Source{{Name: "query.sql", Text: `-- name: Broken :many
DECLARE $seed AS Uint64;
$local = $seed;
SELECT id FROM authors WHERE id = $local OR name = $local;`}},
	)
	require.ErrorContains(t, err, "local $local is constrained by incompatible column types")
}
