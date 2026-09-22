package authors_test

import (
	"slices"
	"testing"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/authors/go/database/sql"
	native "example.com/sqlc-ydb-examples/authors/go/native"
)

func TestAuthorsPagination(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/authors/schema.sql", "DROP TABLE authors;")
	ctx := db.Context
	n, s := native.New(db.Native), sq.New(db.SQL)
	for _, id := range []uint64{40, 10, 30, 20} {
		if err := n.UpsertAuthor(ctx, native.UpsertAuthorParams{AuthorID: id, AuthorName: "Автор"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, runtime := range []struct {
		name string
		page func(int32, uint32) ([]uint64, error)
	}{
		{"native", func(size int32, offset uint32) ([]uint64, error) {
			rows, err := n.ListAuthorsPage(ctx, native.ListAuthorsPageParams{PageSize: size, Offset: offset})
			ids := make([]uint64, len(rows))
			for i, row := range rows {
				ids[i] = row.ID
			}
			return ids, err
		}},
		{"database/sql", func(size int32, offset uint32) ([]uint64, error) {
			rows, err := s.ListAuthorsPage(ctx, sq.ListAuthorsPageParams{PageSize: size, Offset: offset})
			ids := make([]uint64, len(rows))
			for i, row := range rows {
				ids[i] = row.ID
			}
			return ids, err
		}},
	} {
		t.Run(runtime.name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				size   int32
				offset uint32
				want   []uint64
			}{
				{"first page", 2, 0, []uint64{10, 20}},
				{"offset", 2, 1, []uint64{20, 30}},
				{"partial page", 2, 3, []uint64{40}},
				{"zero size", 0, 0, nil},
				{"past end", 2, 4, nil},
				{"offset above signed range", 2, 1 << 31, nil},
				{"maximum signed size", 1<<31 - 1, 0, []uint64{10, 20, 30, 40}},
				{"negative signed size", -1, 1, nil},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got, err := runtime.page(tc.size, tc.offset)
					if err != nil {
						t.Fatal(err)
					}
					if !slices.Equal(got, tc.want) {
						t.Fatalf("page(%d, %d) = %v, want %v", tc.size, tc.offset, got, tc.want)
					}
				})
			}
		})
	}
}
