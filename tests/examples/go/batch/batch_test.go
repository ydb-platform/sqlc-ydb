package batch_test

import (
	"context"
	"path"
	"testing"
	"time"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	batch "example.com/sqlc-ydb-examples/batch/go"
	sq "example.com/sqlc-ydb-examples/batch/go/database/sql"
	native "example.com/sqlc-ydb-examples/batch/go/native"
	"github.com/stretchr/testify/require"
)

func TestBulkUpsertBooks(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/batch/schema.sql", "DROP TABLE books;", "DROP TABLE authors;")

	biography := `{"born":1818,"works":["A Book"]}`
	nativeQueries := native.New(db.Native)
	_, err := nativeQueries.CreateAuthor(db.Context, native.CreateAuthorParams{
		AuthorID: 1, Name: "Unknown Master", Biography: &biography,
	})
	require.NoError(t, err)
	available := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	books := []native.CreateBookParams{
		{BookID: 1, AuthorID: 1, Isbn: "1", BookType: "FICTION", Title: "my book title", Year: 2016, Available: available, Tags: `[]`},
		{BookID: 2, AuthorID: 1, Isbn: "2", BookType: "FICTION", Title: "the second book", Year: 2016, Available: available, Tags: `["cool","unique"]`},
		{BookID: 3, AuthorID: 1, Isbn: "3", BookType: "FICTION", Title: "the third book", Year: 2001, Available: available, Tags: `["cool"]`},
		{BookID: 4, AuthorID: 1, Isbn: "4", BookType: "NONFICTION", Title: "4th place finisher", Year: 2011, Available: available, Tags: `["other"]`},
	}
	require.NoError(t, batch.BulkUpsertBooks(
		db.Context, db.Driver.Table(), path.Join(db.Driver.Name(), "books"), books,
	))

	nativeRows, err := nativeQueries.BooksByYear(db.Context, 2016)
	require.NoError(t, err)
	assertBooks(t, nativeRows, map[uint64]string{1: `[]`, 2: `["cool","unique"]`})

	sqlQueries := sq.New(db.SQL)
	sqlRows, err := sqlQueries.BooksByYear(db.Context, 2016)
	require.NoError(t, err)
	assertSQLBooks(t, sqlRows, map[uint64]string{1: `[]`, 2: `["cool","unique"]`})

	author, err := sqlQueries.GetAuthor(db.Context, 1)
	require.NoError(t, err)
	require.NotNil(t, author.Biography)
	require.JSONEq(t, biography, *author.Biography)
	gotBiography, err := nativeQueries.GetBiography(db.Context, 1)
	require.NoError(t, err)
	require.NotNil(t, gotBiography.Biography)
	require.JSONEq(t, biography, *gotBiography.Biography)
	created, err := nativeQueries.CreateBook(db.Context, native.CreateBookParams{
		BookID: 5, AuthorID: 1, Isbn: "5", BookType: "FICTION", Title: "ordinary insert",
		Year: 2020, Available: available, Tags: `["new"]`,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(5), created.BookID)
	require.NoError(t, sqlQueries.UpdateBook(db.Context, sq.UpdateBookParams{
		BookID: 5, Title: "updated", Tags: `["updated"]`,
	}))
	updated, err := nativeQueries.BooksByYear(db.Context, 2020)
	require.NoError(t, err)
	require.Len(t, updated, 1)
	require.Equal(t, "updated", updated[0].Title)
	require.JSONEq(t, `["updated"]`, updated[0].Tags)

	for _, tc := range []struct {
		name   string
		remove func() error
	}{
		{"exec result", func() error { return sqlQueries.DeleteBookExecResult(db.Context, 1) }},
		{"batch exec", func() error { return nativeQueries.DeleteBook(db.Context, 2) }},
		{"named func", func() error { return sqlQueries.DeleteBookNamedFunc(db.Context, 3) }},
		{"named sign", func() error { return nativeQueries.DeleteBookNamedSign(db.Context, 4) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.remove())
		})
	}
	for _, year := range []int32{2016, 2001, 2011} {
		rows, err := nativeQueries.BooksByYear(db.Context, year)
		require.NoError(t, err)
		require.Empty(t, rows, "year %d", year)
	}

}

func assertBooks(t *testing.T, rows []native.BooksByYearRow, expected map[uint64]string) {
	t.Helper()
	require.Len(t, rows, len(expected))
	for _, row := range rows {
		tags, ok := expected[row.BookID]
		require.True(t, ok, "unexpected native book %d", row.BookID)
		require.JSONEq(t, tags, row.Tags)
	}
}

func assertSQLBooks(t *testing.T, rows []sq.BooksByYearRow, expected map[uint64]string) {
	t.Helper()
	require.Len(t, rows, len(expected))
	for _, row := range rows {
		tags, ok := expected[row.BookID]
		require.True(t, ok, "unexpected database/sql book %d", row.BookID)
		require.JSONEq(t, tags, row.Tags)
	}
}

func TestBulkUpsertEmptyInput(t *testing.T) {
	require.NoError(t, batch.BulkUpsertBooks(context.Background(), nil, "unused", nil))
}

func TestCreateBooksFromStructList(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/batch/schema.sql", "DROP TABLE books;", "DROP TABLE authors;")
	nq, sqlq := native.New(db.Native), sq.New(db.SQL)
	available := time.Date(2026, time.January, 2, 3, 4, 5, 123456000, time.UTC)
	for _, books := range [][]native.CreateBooksBooksItem{nil, {}} {
		require.NoError(t, nq.CreateBooks(db.Context, books), "native empty")
	}
	for _, books := range [][]sq.CreateBooksBooksItem{nil, {}} {
		require.NoError(t, sqlq.CreateBooks(db.Context, books), "database/sql empty")
	}
	nativeBooks := []native.CreateBooksBooksItem{
		{BookID: ^uint64(0), AuthorID: 1, Isbn: "high", BookType: "FICTION", Title: "Unicode ☀", Year: 2026, Available: available, Tags: `{"kind":"native"}`},
		{BookID: 1, AuthorID: 1, Isbn: "one", BookType: "FICTION", Title: "Second", Year: 2026, Available: available, Tags: `[]`},
	}
	require.NoError(t, nq.CreateBooks(db.Context, nativeBooks))
	sqlBooks := []sq.CreateBooksBooksItem{
		{BookID: 2, AuthorID: 1, Isbn: "two", BookType: "REFERENCE", Title: "SQL batch", Year: 2026, Available: available, Tags: `["sql"]`},
		{BookID: 3, AuthorID: 1, Isbn: "three", BookType: "REFERENCE", Title: "Fourth", Year: 2026, Available: available, Tags: `{}`},
	}
	require.NoError(t, sqlq.CreateBooks(db.Context, sqlBooks))
	rows, err := nq.BooksByYear(db.Context, 2026)
	require.NoError(t, err)
	assertBooks(t, rows, map[uint64]string{^uint64(0): `{"kind":"native"}`, 1: `[]`, 2: `["sql"]`, 3: `{}`})
	for _, row := range rows {
		require.True(t, row.Available.Equal(available), "timestamp lost precision: %v", row.Available)
	}
	require.Error(t, nq.CreateBooks(db.Context, nativeBooks[:1]), "native duplicate INSERT must fail")
	require.Error(t, sqlq.CreateBooks(db.Context, sqlBooks[:1]), "database/sql duplicate INSERT must fail")
	cancelled, cancel := context.WithCancel(db.Context)
	cancel()
	require.Error(t, nq.CreateBooks(cancelled, nativeBooks), "native cancelled INSERT succeeded")
	require.Error(t, sqlq.CreateBooks(cancelled, sqlBooks), "database/sql cancelled INSERT succeeded")
}

func TestAuthorsFromNamedStructLists(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/batch/schema.sql", "DROP TABLE books;", "DROP TABLE authors;")
	nq, sqlq := native.New(db.Native), sq.New(db.SQL)
	for _, authors := range [][]native.CreateAuthorsAuthorsItem{nil, {}} {
		require.NoError(t, nq.CreateAuthors(db.Context, authors))
	}
	for _, authors := range [][]sq.CreateAuthorsAuthorsItem{nil, {}} {
		require.NoError(t, sqlq.CreateAuthors(db.Context, authors))
	}
	for _, authors := range [][]native.UpsertAuthorsAuthorsItem{nil, {}} {
		require.NoError(t, nq.UpsertAuthors(db.Context, authors))
	}
	for _, authors := range [][]sq.UpsertAuthorsAuthorsItem{nil, {}} {
		require.NoError(t, sqlq.UpsertAuthors(db.Context, authors))
	}
	require.NoError(t, nq.CreateAuthors(db.Context, []native.CreateAuthorsAuthorsItem{{AuthorID: 1, Name: "Native ☀"}}))
	require.NoError(t, sqlq.CreateAuthors(db.Context, []sq.CreateAuthorsAuthorsItem{{AuthorID: 2, Name: "SQL"}}))
	for id, name := range map[uint64]string{1: "Native ☀", 2: "SQL"} {
		row, err := nq.GetAuthor(db.Context, id)
		require.NoError(t, err)
		require.Equal(t, id, row.AuthorID)
		require.Equal(t, name, row.Name)
		require.Nil(t, row.Biography)
	}
	biography := `{"preserve":true}`
	_, err := nq.CreateAuthor(db.Context, native.CreateAuthorParams{AuthorID: 3, Name: "Original", Biography: &biography})
	require.NoError(t, err)
	require.NoError(t, nq.UpsertAuthors(db.Context, []native.UpsertAuthorsAuthorsItem{
		{AuthorID: 1, Name: "Updated native"}, {AuthorID: 3, Name: "Preserved native"},
	}))
	require.NoError(t, sqlq.UpsertAuthors(db.Context, []sq.UpsertAuthorsAuthorsItem{
		{AuthorID: 2, Name: "Updated SQL"}, {AuthorID: 3, Name: "Preserved SQL"}, {AuthorID: 4, Name: "New SQL"},
	}))
	for id, name := range map[uint64]string{1: "Updated native", 2: "Updated SQL", 3: "Preserved SQL", 4: "New SQL"} {
		row, err := sqlq.GetAuthor(db.Context, id)
		require.NoError(t, err)
		require.Equal(t, id, row.AuthorID)
		require.Equal(t, name, row.Name)
		if id == 3 {
			require.NotNil(t, row.Biography)
			require.JSONEq(t, biography, *row.Biography)
		} else {
			require.Nil(t, row.Biography)
		}
	}
}
