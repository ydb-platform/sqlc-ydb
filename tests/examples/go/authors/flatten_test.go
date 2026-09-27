package authors_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/authors/go/database/sql"
	native "example.com/sqlc-ydb-examples/authors/go/native"
)

func TestAuthorNameWords(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/authors/schema.sql", "DROP TABLE authors;")
	n, s := native.New(db.Native), sq.New(db.SQL)
	for _, author := range []native.UpsertAuthorParams{
		{AuthorID: 1, AuthorName: "Ada Lovelace"},
		{AuthorID: 2, AuthorName: "Grace Hopper"},
	} {
		require.NoError(t, n.UpsertAuthor(db.Context, author))
	}
	want := []native.ListAuthorNameWordsRow{
		{ID: 1, Word: "Ada"}, {ID: 1, Word: "Lovelace"},
		{ID: 2, Word: "Grace"}, {ID: 2, Word: "Hopper"},
	}
	got, err := n.ListAuthorNameWords(db.Context)
	require.NoError(t, err)
	require.Equal(t, want, got)
	sqlRows, err := s.ListAuthorNameWords(db.Context)
	require.NoError(t, err)
	require.Len(t, sqlRows, len(want))
	for i, row := range sqlRows {
		require.Equal(t, want[i].ID, row.ID)
		require.Equal(t, want[i].Word, row.Word)
	}
}
