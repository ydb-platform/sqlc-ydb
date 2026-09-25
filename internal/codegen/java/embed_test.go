package java

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
-- name: GetDeclared :one
DECLARE $id AS Uint64;
SELECT sqlc.embed(b), sqlc.embed(a) FROM books AS b INNER JOIN authors AS a ON b.author_id = a.author_id WHERE b.id = $id;
-- name: ListMixed :many
SELECT b.id AS front, sqlc.embed(b), b.author_id AS middle, sqlc.embed(a), a.name AS tail FROM books AS b INNER JOIN authors AS a ON b.author_id = a.author_id;`}},
	)
	require.NoError(t, err)
	for _, runtime := range []string{"ydb", "jdbc", "jooq"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(analysis, Options{Package: "embed", Runtime: runtime})
			require.NoError(t, err)
			generated := map[string]string{}
			for _, file := range files {
				generated[file.Name] = string(file.Content)
			}
			require.Contains(t, generated["GetEmbeddedRow.java"], "Books books, Authors authors")
			require.Contains(t, generated["GetDeclaredRow.java"], "Books books, Authors authors")
			require.Contains(t, generated["Books.java"], "authorId")
			require.Contains(t, generated["Authors.java"], "authorId")
			queries := generated["Queries.java"]
			require.Contains(t, queries, "PRAGMA OrderedColumns;")
			if runtime == "jooq" {
				require.Contains(t, generated["ListMixedRow.java"], "ULong front, Books books, ULong middle, Authors authors, String tail")
				require.Contains(t, queries, "dsl.resultQuery(\"{0};\\n{1}\"")
				require.Contains(t, queries, "new GetEmbeddedRow(new Books(")
				require.Contains(t, queries, "new Authors(")
				require.Contains(t, queries, `_record.get(1, org.jooq.types.ULong.class)`)
				require.Contains(t, queries, `_record.get(2, org.jooq.types.ULong.class)`)
				require.Contains(t, queries, "new GetDeclaredRow(new Books(")
				require.Contains(t, queries, "new ListMixedRow(_record.get(0, org.jooq.types.ULong.class), new Books(_record.get(1, org.jooq.types.ULong.class), _record.get(2, org.jooq.types.ULong.class)), _record.get(3, org.jooq.types.ULong.class), new Authors(_record.get(4, org.jooq.types.ULong.class), _record.get(5, String.class)), _record.get(6, String.class))")
			} else {
				require.Contains(t, generated["ListMixedRow.java"], "long front, Books books, long middle, Authors authors, String tail")
				require.Contains(t, queries, "new GetEmbeddedRow(new Books(_value0, _value1), new Authors(_value2, _value3))")
				require.Contains(t, queries, "new ListMixedRow(_value0, new Books(_value1, _value2), _value3, new Authors(_value4, _value5), _value6)")
			}
			require.Equal(t, 1, strings.Count(queries, "new GetEmbeddedRow("))
		})
	}
}
