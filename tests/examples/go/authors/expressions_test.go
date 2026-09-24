package authors_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/authors/go/database/sql"
	native "example.com/sqlc-ydb-examples/authors/go/native"
)

func TestAuthorExpressions(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/authors/schema.sql", "DROP TABLE authors;")
	ctx := db.Context
	n, s := native.New(db.Native), sq.New(db.SQL)
	type statistics struct {
		total, withBio, withNonemptyBio uint64
		exists                          bool
	}
	readers := []struct {
		name   string
		stats  func() (statistics, error)
		prefix func(string) ([]bool, error)
	}{
		{"native", func() (statistics, error) {
			r, err := n.GetAuthorStatistics(ctx)
			return statistics{r.Total, r.WithBio, r.WithNonemptyBio, r.Column3}, err
		}, func(prefix string) ([]bool, error) {
			rows, err := n.FindAuthorsByNamePrefix(ctx, prefix)
			flags := make([]bool, len(rows))
			for i, row := range rows {
				flags[i] = row.HasBio
			}
			return flags, err
		}},
		{"database/sql", func() (statistics, error) {
			r, err := s.GetAuthorStatistics(ctx)
			return statistics{r.Total, r.WithBio, r.WithNonemptyBio, r.Column3}, err
		}, func(prefix string) ([]bool, error) {
			rows, err := s.FindAuthorsByNamePrefix(ctx, prefix)
			flags := make([]bool, len(rows))
			for i, row := range rows {
				flags[i] = row.HasBio
			}
			return flags, err
		}},
	}
	for _, reader := range readers {
		got, err := reader.stats()
		require.NoError(t, err, reader.name)
		require.Equal(t, statistics{}, got, reader.name)
	}
	empty, bio := "", "biography"
	for _, row := range []native.UpsertAuthorParams{
		{AuthorID: 1, AuthorName: "Alice"},
		{AuthorID: 2, AuthorName: "Alfred", Biography: &empty},
		{AuthorID: ^uint64(0), AuthorName: "Bob", Biography: &bio},
	} {
		require.NoError(t, n.UpsertAuthor(ctx, row))
	}
	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			got, err := reader.stats()
			require.NoError(t, err)
			require.Equal(t, statistics{3, 2, 1, true}, got)
			prefix, err := reader.prefix("Al")
			require.NoError(t, err)
			require.Equal(t, []bool{false, true}, prefix)
			missing, err := reader.prefix("missing")
			require.NoError(t, err)
			require.Empty(t, missing)
		})
	}
	for _, id := range []uint64{1, ^uint64(0)} {
		want := uint32(0)
		if id == 1 {
			want = 1
		}
		nativeRow, err := n.GetAuthorExportMetadata(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, nativeRow.Column6)
		require.False(t, nativeRow.ExportTimestamp.IsZero())
		sqlRow, err := s.GetAuthorExportMetadata(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, sqlRow.Column6)
		require.False(t, sqlRow.ExportTimestamp.IsZero())
	}
}
