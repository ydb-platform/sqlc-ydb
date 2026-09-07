package authors_test

import (
	"testing"

	sq "example.com/sqlc-ydb-examples/authors/go/database/sql"
	native "example.com/sqlc-ydb-examples/authors/go/native"
	"example.com/sqlc-ydb-examples/internal/testdb"
)

// TestGeneratedExample executes the actual CLI-generated example. It requires a
// disposable database without an existing authors table; it never drops an
// existing table when setup fails.
func TestGeneratedExample(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../schema.sql", "DROP TABLE authors;")
	ctx := db.Context
	n, s := native.New(db.Native), sq.New(db.SQL)
	for _, tc := range []struct {
		name       string
		create     func(uint64, string, *string) (string, *string, error)
		put        func(uint64, string, *string) error
		get        func(uint64) (string, *string, error)
		projection func(uint64) (string, error)
		list       func() (int, error)
		remove     func(uint64) error
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
			func() (int, error) { r, e := n.ListAuthors(ctx); return len(r), e },
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
			func() (int, error) { r, e := s.ListAuthors(ctx); return len(r), e },
			func(id uint64) error { return s.DeleteAuthor(ctx, id) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := ^uint64(0)
			if name, bio, err := tc.create(id, "Автор", nil); err != nil || name != "Автор" || bio != nil {
				t.Fatalf("insert returning: %q %v %v", name, bio, err)
			}
			if err := tc.put(id, "Автор", nil); err != nil {
				t.Fatal(err)
			}
			if name, bio, err := tc.get(id); err != nil || name != "Автор" || bio != nil {
				t.Fatalf("read: %q %v %v", name, bio, err)
			}
			if name, err := tc.projection(id); err != nil || name != "Автор" {
				t.Fatalf("projection: %q %v", name, err)
			}
			bio := "Биография"
			if err := tc.put(id, "Автор", &bio); err != nil {
				t.Fatal(err)
			}
			if _, got, err := tc.get(id); err != nil || got == nil || *got != bio {
				t.Fatalf("optional: %v %v", got, err)
			}
			if count, err := tc.list(); err != nil || count != 1 {
				t.Fatalf("many: %d %v", count, err)
			}
			if err := tc.remove(id); err != nil {
				t.Fatal(err)
			}
			if _, _, err := tc.get(id); err == nil {
				t.Fatal("expected missing-row error")
			}
		})
	}
}
