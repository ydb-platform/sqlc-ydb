package authors_test

import (
	"reflect"
	"testing"

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
		if got, err := reader.stats(); err != nil || got != (statistics{}) {
			t.Fatalf("%s empty aggregate: %#v, %v", reader.name, got, err)
		}
	}
	empty, bio := "", "biography"
	for _, row := range []native.UpsertAuthorParams{
		{AuthorID: 1, AuthorName: "Alice"},
		{AuthorID: 2, AuthorName: "Alfred", Biography: &empty},
		{AuthorID: ^uint64(0), AuthorName: "Bob", Biography: &bio},
	} {
		if err := n.UpsertAuthor(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			if got, err := reader.stats(); err != nil || got != (statistics{3, 2, 1, true}) {
				t.Fatalf("aggregate: %#v, %v", got, err)
			}
			if got, err := reader.prefix("Al"); err != nil || !reflect.DeepEqual(got, []bool{false, true}) {
				t.Fatalf("prefix: %v, %v", got, err)
			}
			if got, err := reader.prefix("missing"); err != nil || len(got) != 0 {
				t.Fatalf("missing prefix: %v, %v", got, err)
			}
		})
	}
	for _, id := range []uint64{1, ^uint64(0)} {
		want := uint32(0)
		if id == 1 {
			want = 1
		}
		nativeRow, err := n.GetAuthorExportMetadata(ctx, id)
		if err != nil || nativeRow.Column6 != want || nativeRow.ExportTimestamp.IsZero() {
			t.Fatalf("native export %d: %#v, %v", id, nativeRow, err)
		}
		sqlRow, err := s.GetAuthorExportMetadata(ctx, id)
		if err != nil || sqlRow.Column6 != want || sqlRow.ExportTimestamp.IsZero() {
			t.Fatalf("database/sql export %d: %#v, %v", id, sqlRow, err)
		}
	}
}
