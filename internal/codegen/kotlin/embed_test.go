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
