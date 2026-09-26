package kotlin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestEmbeddedTableRows(t *testing.T) {
	analysis, err := analyzer.Analyze(
		[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, author_id Uint64 NOT NULL, PRIMARY KEY(id)); CREATE TABLE authors (author_id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(author_id));"}},
		[]model.Source{{Name: "query.sql", Text: `-- name: GetEmbedded :one
SELECT sqlc.embed(b), sqlc.embed(a) FROM books AS b INNER JOIN authors AS a ON b.author_id = a.author_id WHERE b.id = $id;
-- name: ListMixed :many
SELECT b.id AS front, sqlc.embed(b), b.author_id AS middle, sqlc.embed(a), a.name AS tail FROM books AS b INNER JOIN authors AS a ON b.author_id = a.author_id;`}},
	)
	require.NoError(t, err)
	for _, runtime := range []string{"ydb", "jdbc", "exposed"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(analysis, Options{Package: "embed", Runtime: runtime})
			require.NoError(t, err)
			generated := map[string]string{}
			for _, file := range files {
				generated[file.Name] = string(file.Content)
			}
			require.Contains(t, generated["GetEmbeddedRow.kt"], "val books: Books")
			require.Contains(t, generated["GetEmbeddedRow.kt"], "val authors: Authors")
			require.Contains(t, generated["ListMixedRow.kt"], "val front: Long,\n    val books: Books,\n    val middle: Long,\n    val authors: Authors,\n    val tail: String")
			require.Contains(t, generated["Books.kt"], "val authorId: Long")
			require.Contains(t, generated["Authors.kt"], "val authorId: Long")
			queries := generated["Queries.kt"]
			require.Contains(t, queries, "GetEmbeddedRow(Books(_value0, _value1), Authors(_value2, _value3))")
			require.Contains(t, queries, "ListMixedRow(_value0, Books(_value1, _value2), _value3, Authors(_value4, _value5), _value6)")
			require.Equal(t, 1, strings.Count(queries, "GetEmbeddedRow(Books("))
			if runtime == "exposed" {
				require.Contains(t, queries, "client.connection.connection as java.sql.Connection")
			}
		})
	}
}

func TestEmbeddedRowsRejectInvalidModel(t *testing.T) {
	u64 := model.Type{Kind: "Uint64"}
	for _, tc := range []struct {
		name   string
		change func(*model.AnalysisResult)
		want   string
	}{
		{"row type collision", func(a *model.AnalysisResult) {
			a.Catalog.Tables[0].Name = "ReadRow"
			a.Queries[0].ResultSets[0].Embeds[0].Table = "ReadRow"
		}, "Kotlin type name collision: ReadRow"},
		{"invalid embedded model name", func(a *model.AnalysisResult) { a.Queries[0].ResultSets[0].Embeds[0].Table = "!" }, "cannot represent"},
		{"unsupported scalar beside embed", func(a *model.AnalysisResult) {
			a.Queries[0].ResultSets[0].Columns = append(a.Queries[0].ResultSets[0].Columns, model.Column{Name: "extra", Type: model.Type{Kind: "Tuple"}})
		}, "unsupported Kotlin type Tuple"},
		{"invalid embedded field", func(a *model.AnalysisResult) { a.Queries[0].ResultSets[0].Embeds[0].Field = "!" }, "cannot represent"},
		{"duplicate row field", func(a *model.AnalysisResult) {
			a.Queries[0].ResultSets[0].Columns = append(a.Queries[0].ResultSets[0].Columns, model.Column{Name: "books", Type: u64})
		}, "Kotlin field name collision in ReadRow: books"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &model.AnalysisResult{
				Catalog: model.Catalog{Tables: []model.Table{{Name: "books", Columns: []model.Column{{Name: "id", Type: u64}}}}},
				Queries: []model.AnalyzedQuery{{Name: "Read", Command: model.One, SQL: "SELECT id FROM books;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: u64}}, Embeds: []model.Embedding{{Start: 0, End: 1, Table: "books", Field: "books"}}}}}},
			}
			tc.change(a)
			_, err := Generate(a, Options{Runtime: "ydb"})
			require.ErrorContains(t, err, tc.want)
		})
	}
}
