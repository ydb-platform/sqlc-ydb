package authors_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/authors/go/database/sql"
	native "example.com/sqlc-ydb-examples/authors/go/native"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
)

// TestGeneratedExample executes the actual CLI-generated example. It requires a
// disposable database without an existing authors table; it never drops an
// existing table when setup fails.
func TestGeneratedExample(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/authors/schema.sql", "DROP TABLE authors;")
	ctx := db.Context
	n, s := native.New(db.Native), sq.New(db.SQL)
	for _, tc := range []struct {
		name           string
		create         func(uint64, string, *string) (string, *string, error)
		put            func(uint64, string, *string) error
		get            func(uint64) (string, *string, error)
		projection     func(uint64) (string, error)
		echo           func(string) (string, error)
		list           func() (int, error)
		listWithoutBio func() ([]uint64, error)
		remove         func(uint64) error
	}{
		{"native",
			func(id uint64, name string, bio *string) (string, *string, error) {
				r, err := n.CreateAuthor(ctx, native.CreateAuthorParams{AuthorID: id, AuthorName: name, Biography: bio})
				return r.Name, r.Bio, err
			},
			func(id uint64, name string, bio *string) error {
				return n.UpsertAuthor(ctx, native.UpsertAuthorParams{AuthorID: id, AuthorName: name, Biography: bio})
			},
			func(id uint64) (string, *string, error) { r, e := n.GetAuthor(ctx, id); return r.Name, r.Bio, e },
			func(id uint64) (string, error) { r, e := n.GetAuthorName(ctx, id); return r.Name, e },
			func(id string) (string, error) { r, e := n.EchoAuthorIDText(ctx, id); return r.AuthorIDText, e },
			func() (int, error) { r, e := n.ListAuthors(ctx); return len(r), e },
			func() ([]uint64, error) {
				rows, err := n.ListAuthorsWithoutBio(ctx)
				var ids []uint64
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
				return ids, err
			},
			func(id uint64) error { return n.DeleteAuthor(ctx, id) },
		},
		{"database/sql",
			func(id uint64, name string, bio *string) (string, *string, error) {
				r, err := s.CreateAuthor(ctx, sq.CreateAuthorParams{AuthorID: id, AuthorName: name, Biography: bio})
				return r.Name, r.Bio, err
			},
			func(id uint64, name string, bio *string) error {
				return s.UpsertAuthor(ctx, sq.UpsertAuthorParams{AuthorID: id, AuthorName: name, Biography: bio})
			},
			func(id uint64) (string, *string, error) { r, e := s.GetAuthor(ctx, id); return r.Name, r.Bio, e },
			func(id uint64) (string, error) { r, e := s.GetAuthorName(ctx, id); return r.Name, e },
			func(id string) (string, error) { r, e := s.EchoAuthorIDText(ctx, id); return r.AuthorIDText, e },
			func() (int, error) { r, e := s.ListAuthors(ctx); return len(r), e },
			func() ([]uint64, error) {
				rows, err := s.ListAuthorsWithoutBio(ctx)
				var ids []uint64
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
				return ids, err
			},
			func(id uint64) error { return s.DeleteAuthor(ctx, id) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := ^uint64(0)
			name, bio, err := tc.create(id, "Автор", nil)
			require.NoError(t, err)
			require.Equal(t, "Автор", name)
			require.Nil(t, bio)
			require.NoError(t, tc.put(id, "Автор", nil))
			name, bio, err = tc.get(id)
			require.NoError(t, err)
			require.Equal(t, "Автор", name)
			require.Nil(t, bio)
			name, err = tc.projection(id)
			require.NoError(t, err)
			require.Equal(t, "Автор", name)
			textID, err := tc.echo("external-id")
			require.NoError(t, err)
			require.Equal(t, "external-id", textID)
			biography := "Биография"
			require.NoError(t, tc.put(id, "Автор", &biography))
			_, got, err := tc.get(id)
			require.NoError(t, err)
			require.Equal(t, &biography, got)
			count, err := tc.list()
			require.NoError(t, err)
			require.Equal(t, 1, count)
			ids, err := tc.listWithoutBio()
			require.NoError(t, err)
			require.Equal(t, []uint64{id}, ids)
			require.NoError(t, tc.remove(id))
			_, _, err = tc.get(id)
			require.Error(t, err, "expected missing-row error")
		})
	}
}

// TestSharedTransactions verifies read-your-writes and rollback for both runtimes.
func TestSharedTransactions(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/authors/schema.sql", "DROP TABLE authors;")
	ctx := db.Context
	aborted := errors.New("rollback generated calls")
	err := db.Native.DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		q := native.New(tx)
		if err := q.UpsertAuthor(ctx, native.UpsertAuthorParams{AuthorID: 42, AuthorName: "transaction"}); err != nil {
			return err
		}
		row, err := q.GetAuthor(ctx, 42)
		if err != nil {
			return err
		}
		if row.Name != "transaction" {
			return errors.New("transaction did not read its write")
		}
		return aborted
	})
	require.ErrorIs(t, err, aborted)
	rows, err := native.New(db.Native).ListAuthors(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
	tx, err := db.SQL.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() {
		err := tx.Rollback()
		assert.True(t, err == nil || errors.Is(err, sql.ErrTxDone), "rollback cleanup: %v", err)
	}()
	q := sq.New(db.SQL).WithTx(tx)
	require.NoError(t, q.UpsertAuthor(ctx, sq.UpsertAuthorParams{AuthorID: 42, AuthorName: "transaction"}))
	row, err := q.GetAuthor(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, "transaction", row.Name)
	require.NoError(t, tx.Rollback())
	_, err = sq.New(db.SQL).GetAuthor(ctx, 42)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestGeneratedIndexQueries(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/authors/schema.sql", "DROP TABLE authors;")
	ctx := db.Context
	n, s := native.New(db.Native), sq.New(db.SQL)
	bio := "covered biography"
	for _, value := range []native.UpsertAuthorParams{{AuthorID: 1, AuthorName: "same", Biography: &bio}, {AuthorID: 2, AuthorName: "same"}, {AuthorID: 3, AuthorName: "other"}} {
		require.NoError(t, n.UpsertAuthor(ctx, value))
	}
	for _, tc := range []struct {
		name string
		read func(string) ([]string, error)
	}{
		{"native/noncovering", func(name string) ([]string, error) {
			rows, e := n.FindAuthorsByName(ctx, name)
			var out []string
			for _, r := range rows {
				if r.Bio != nil {
					out = append(out, *r.Bio)
				} else {
					out = append(out, "NULL")
				}
			}
			return out, e
		}},
		{"native/covering", func(name string) ([]string, error) {
			rows, e := n.FindAuthorsByNameCovering(ctx, name)
			var out []string
			for _, r := range rows {
				if r.Bio != nil {
					out = append(out, *r.Bio)
				} else {
					out = append(out, "NULL")
				}
			}
			return out, e
		}},
		{"database/sql/noncovering", func(name string) ([]string, error) {
			rows, e := s.FindAuthorsByName(ctx, name)
			var out []string
			for _, r := range rows {
				if r.Bio != nil {
					out = append(out, *r.Bio)
				} else {
					out = append(out, "NULL")
				}
			}
			return out, e
		}},
		{"database/sql/covering", func(name string) ([]string, error) {
			rows, e := s.FindAuthorsByNameCovering(ctx, name)
			var out []string
			for _, r := range rows {
				if r.Bio != nil {
					out = append(out, *r.Bio)
				} else {
					out = append(out, "NULL")
				}
			}
			return out, e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := tc.read("same")
			require.NoError(t, err)
			require.Equal(t, []string{bio, "NULL"}, rows)
			rows, err = tc.read("missing")
			require.NoError(t, err)
			require.Empty(t, rows)
		})
	}
}
