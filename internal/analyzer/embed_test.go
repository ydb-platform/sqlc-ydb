package analyzer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

var embedSchema = []model.Source{{Name: "schema.sql", Text: `
CREATE TABLE books (book_id Uint64 NOT NULL, author_id Uint64 NOT NULL, title Utf8, PRIMARY KEY(book_id));
CREATE TABLE authors (author_id Uint64 NOT NULL, name Utf8, PRIMARY KEY(author_id));`}}

func TestAnalyzeExpandsEmbeddedTableResults(t *testing.T) {
	query := "-- name: Read :many\nSELECT sqlc.embed(b), b.author_id AS selected_author_id, sqlc.embed(a) FROM books AS b JOIN authors AS a ON b.author_id = a.author_id;"
	result, err := Analyze(embedSchema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Len(t, result.Queries, 1)
	got := result.Queries[0]
	require.Equal(t, []model.Embedding{
		{Start: 0, End: 3, Table: "books", Alias: "b", Field: "books"},
		{Start: 4, End: 6, Table: "authors", Alias: "a", Field: "authors"},
	}, got.ResultSets[0].Embeds)
	require.Equal(t, []string{"book_id", "author_id", "title", "selected_author_id", "author_id", "name"}, []string{
		got.ResultSets[0].Columns[0].Name, got.ResultSets[0].Columns[1].Name, got.ResultSets[0].Columns[2].Name,
		got.ResultSets[0].Columns[3].Name, got.ResultSets[0].Columns[4].Name, got.ResultSets[0].Columns[5].Name,
	})
	require.Equal(t, "__sqlc_embed_0_0", got.ResultSets[0].Columns[0].ResultName())
	require.Equal(t, "selected_author_id", got.ResultSets[0].Columns[3].ResultName())
	require.Equal(t, "__sqlc_embed_2_1", got.ResultSets[0].Columns[5].ResultName())
	require.Contains(t, got.SQL, "`b`.`book_id` AS `__sqlc_embed_0_0`")
	require.Contains(t, got.SQL, "`a`.`author_id` AS `__sqlc_embed_2_0`")
	require.Contains(t, got.SQL, "-- name: Read :many\nPRAGMA OrderedColumns;\nSELECT")
	require.NotContains(t, got.SQL, "sqlc.embed")
}

func TestAnalyzeRejectsUnsupportedEmbedForms(t *testing.T) {
	tests := []struct{ name, sql, diagnostic string }{
		{"unknown alias", "SELECT sqlc.embed(missing) FROM books b;", "unknown table or alias"},
		{"argument expression", "SELECT sqlc.embed(b.book_id) FROM books b;", "expects a table or relation alias"},
		{"multiple arguments", "SELECT sqlc.embed(b, b) FROM books b;", "expects one table or relation alias"},
		{"result alias", "SELECT sqlc.embed(b) AS nested FROM books b;", "cannot have an AS alias"},
		{"outer join", "SELECT sqlc.embed(a) FROM books b LEFT JOIN authors a ON b.author_id = a.author_id;", "nullable OUTER JOIN side"},
		{"derived table", "SELECT sqlc.embed(x) FROM (SELECT book_id FROM books) AS x;", "requires a physical catalog table"},
		{"self join", "SELECT sqlc.embed(a), sqlc.embed(b) FROM books a JOIN books b ON a.book_id = b.book_id;", "repeats logical result field"},
		{"nested select", "SELECT x.book_id FROM (SELECT sqlc.embed(b) FROM books b) AS x;", "single top-level SELECT result"},
		{"nested call", "SELECT COALESCE(sqlc.embed(b), 1) FROM books b;", "must be a direct result expression"},
		{"member after embed", "SELECT sqlc.embed(b).book_id FROM books b;", "must be a direct result expression"},
		{"where call", "SELECT book_id FROM books b WHERE sqlc.embed(b) = 1;", "must be a direct result expression"},
		{"union", "SELECT sqlc.embed(b) FROM books b UNION ALL SELECT sqlc.embed(b) FROM books b;", "single top-level SELECT"},
		{"multiple statements", "SELECT sqlc.embed(b) FROM books b; SELECT book_id FROM books;", "multi-statement queries support at most one result-producing statement"},
		{"computed scalar", "SELECT sqlc.embed(b), b.book_id + 1 FROM books b;", "requires explicit AS aliases"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Analyze(embedSchema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tt.sql}})
			require.Error(t, err)
			require.NotEmpty(t, result.Diagnostics)
			require.Contains(t, err.Error(), tt.diagnostic)
		})
	}
}

func TestAnalyzeRejectsEmbedsWithMatchingTableBaseNames(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/one/books` (id Uint64 NOT NULL, PRIMARY KEY(id)); CREATE TABLE `/two/books` (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	query := "-- name: Read :many\nSELECT sqlc.embed(a), sqlc.embed(b) FROM `/one/books` AS a JOIN `/two/books` AS b ON a.id = b.id;"
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.ErrorContains(t, err, `repeats logical result field "books"`)
}

func TestAnalyzeEmbeddedEachAndLiteralText(t *testing.T) {
	query := "-- name: Read :each\nSELECT 'sqlc.embed(b)' AS literal, sqlc.embed(b) FROM books b /* sqlc.embed(a) */;"
	result, err := Analyze(embedSchema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, model.Each, result.Queries[0].Command)
	require.Equal(t, "literal", result.Queries[0].ResultSets[0].Columns[0].Name)
	require.Equal(t, model.Embedding{Start: 1, End: 4, Table: "books", Alias: "b", Field: "books"}, result.Queries[0].ResultSets[0].Embeds[0])
	require.Contains(t, result.Queries[0].SQL, "'sqlc.embed(b)'")
	require.Contains(t, result.Queries[0].SQL, "/* sqlc.embed(a) */")
}

func TestAnalyzeEmbeddedRewritePreservesSurroundingSQL(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `путь/books` (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	query := "-- name: Read :one\r\nSELECT 'sqlc.embed(b)' AS marker, /* перед */ sqlc . embed ( b ) /* после */ FROM `путь/books` AS b;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, "-- name: Read :one\r\nPRAGMA OrderedColumns;\r\nSELECT 'sqlc.embed(b)' AS marker, /* перед */ `b`.`id` AS `__sqlc_embed_1_0` /* после */ FROM `путь/books` AS b;", result.Queries[0].SQL)
	require.Equal(t, model.Embedding{Start: 1, End: 2, Table: "путь/books", Alias: "b", Field: "books"}, result.Queries[0].ResultSets[0].Embeds[0])
}

func TestAnalyzeEmbeddedOrderingWithExistingPragmas(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE `/local/books` (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	for _, preamble := range []string{
		"PRAGMA TablePathPrefix('/local');\nDECLARE $id AS Uint64;\n",
		"PRAGMA OrderedColumns;\nPRAGMA TablePathPrefix('/local');\nDECLARE $id AS Uint64;\n",
	} {
		query := "-- name: Read :one\n" + preamble + "SELECT sqlc.embed(b) FROM books AS b WHERE b.id = $id;"
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
		require.NoError(t, err)
		got := result.Queries[0]
		require.Equal(t, 1, strings.Count(got.SQL, "PRAGMA OrderedColumns;"))
		require.Contains(t, got.SQL, "PRAGMA TablePathPrefix('/local');\nDECLARE $id AS Uint64;\nSELECT `b`.`id` AS `__sqlc_embed_0_0` FROM books AS b")
		require.Equal(t, "/local/books", got.ResultSets[0].Embeds[0].Table)
	}
}

func TestAnalyzeRejectsLateOrDisabledColumnOrdering(t *testing.T) {
	for _, query := range []string{
		"PRAGMA OrderedColumns = default; SELECT sqlc.embed(b) FROM books AS b;",
		"SELECT book_id FROM books; PRAGMA OrderedColumns;",
		"PRAGMA DisableOrderedColumns; SELECT sqlc.embed(b) FROM books AS b;",
	} {
		_, err := Analyze(embedSchema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + query}})
		require.ErrorContains(t, err, "PRAGMA")
	}
}

func TestDatabaseValidatesExpandedEmbeddedSQL(t *testing.T) {
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{
		"books": {Name: "books", Columns: []model.Column{{Name: "book_id", Type: model.Type{Kind: "Uint64"}, Table: "books"}, {Name: "author_id", Type: model.Type{Kind: "Uint64"}, Table: "books"}, {Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"}), Table: "books"}}, PrimaryKey: []string{"book_id"}},
	}}
	query := "-- name: Read :many\nSELECT sqlc.embed(b) FROM books b;"
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: query}}, Options{}, database)
	require.NoError(t, err)
	require.Len(t, database.validated, 1)
	require.NotContains(t, database.validated[0], "sqlc.embed")
	require.Contains(t, database.validated[0], "PRAGMA OrderedColumns;")
	require.True(t, strings.Contains(database.validated[0], "__sqlc_embed_0_0"))
	require.Equal(t, database.validated[0], result.Queries[0].SQL)
}

func TestDatabaseReportsInvalidEmbedSyntaxBeforeValidation(t *testing.T) {
	database := &fakeAnalysisDatabase{}
	query := "-- name: Read :many\nSELECT sqlc.embed(b) FROM books AS b WHERE ("
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: query}}, Options{}, database)
	require.Error(t, err)
	require.Contains(t, err.Error(), "query.sql:2:")
	require.Empty(t, database.validated)
	require.Empty(t, database.described)
}

func TestDatabaseReportsExpandedEmbedValidationFailure(t *testing.T) {
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{
		"books": {Name: "books", Columns: []model.Column{{Name: "book_id", Type: model.Type{Kind: "Uint64"}, Table: "books"}}, PrimaryKey: []string{"book_id"}},
	}, validateError: errors.New("rejected expanded query")}
	query := "-- name: Read :many\nSELECT sqlc.embed(b) FROM books AS b;"
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: query}}, Options{}, database)
	require.ErrorContains(t, err, "database query validation failed: rejected expanded query")
	require.Len(t, database.validated, 1)
	require.Contains(t, database.validated[0], "__sqlc_embed_0_0")
}
