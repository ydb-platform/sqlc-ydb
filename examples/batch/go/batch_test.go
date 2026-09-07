package batch_test

import (
	"context"
	"encoding/json"
	"path"
	"reflect"
	"testing"
	"time"

	batch "example.com/sqlc-ydb-examples/batch/go"
	sq "example.com/sqlc-ydb-examples/batch/go/database/sql"
	native "example.com/sqlc-ydb-examples/batch/go/native"
	"example.com/sqlc-ydb-examples/internal/testdb"
)

func TestBulkUpsertBooks(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../schema.sql", "DROP TABLE books;", "DROP TABLE authors;")

	biography := `{"born":1818,"works":["A Book"]}`
	nativeQueries := native.New(db.Native)
	if _, err := nativeQueries.CreateAuthor(db.Context, native.CreateAuthorParams{
		AuthorID: 1, Name: "Unknown Master", Biography: &biography,
	}); err != nil {
		t.Fatal(err)
	}
	available := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	books := []native.CreateBookParams{
		{BookID: 1, AuthorID: 1, Isbn: "1", BookType: "FICTION", Title: "my book title", Year: 2016, Available: available, Tags: `[]`},
		{BookID: 2, AuthorID: 1, Isbn: "2", BookType: "FICTION", Title: "the second book", Year: 2016, Available: available, Tags: `["cool","unique"]`},
		{BookID: 3, AuthorID: 1, Isbn: "3", BookType: "FICTION", Title: "the third book", Year: 2001, Available: available, Tags: `["cool"]`},
		{BookID: 4, AuthorID: 1, Isbn: "4", BookType: "NONFICTION", Title: "4th place finisher", Year: 2011, Available: available, Tags: `["other"]`},
	}
	if err := batch.BulkUpsertBooks(
		db.Context, db.Driver.Table(), path.Join(db.Driver.Name(), "books"), books,
	); err != nil {
		t.Fatal(err)
	}

	nativeRows, err := nativeQueries.BooksByYear(db.Context, 2016)
	if err != nil {
		t.Fatal(err)
	}
	assertBooks(t, nativeRows, map[uint64]string{1: `[]`, 2: `["cool","unique"]`})

	sqlQueries := sq.New(db.SQL)
	sqlRows, err := sqlQueries.BooksByYear(db.Context, 2016)
	if err != nil {
		t.Fatal(err)
	}
	assertSQLBooks(t, sqlRows, map[uint64]string{1: `[]`, 2: `["cool","unique"]`})

	author, err := sqlQueries.GetAuthor(db.Context, 1)
	if err != nil || author.Biography == nil || !jsonEqual(*author.Biography, biography) {
		t.Fatalf("database/sql author = %#v, err = %v", author, err)
	}
	gotBiography, err := nativeQueries.GetBiography(db.Context, 1)
	if err != nil || gotBiography.Biography == nil || !jsonEqual(*gotBiography.Biography, biography) {
		t.Fatalf("native biography = %#v, err = %v", gotBiography, err)
	}
	created, err := nativeQueries.CreateBook(db.Context, native.CreateBookParams{
		BookID: 5, AuthorID: 1, Isbn: "5", BookType: "FICTION", Title: "ordinary insert",
		Year: 2020, Available: available, Tags: `["new"]`,
	})
	if err != nil || created.BookID != 5 {
		t.Fatalf("CreateBook = %#v, err = %v", created, err)
	}
	if err := sqlQueries.UpdateBook(db.Context, sq.UpdateBookParams{
		BookID: 5, Title: "updated", Tags: `["updated"]`,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := nativeQueries.BooksByYear(db.Context, 2020)
	if err != nil || len(updated) != 1 || updated[0].Title != "updated" || !jsonEqual(updated[0].Tags, `["updated"]`) {
		t.Fatalf("updated rows = %#v, err = %v", updated, err)
	}

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
			if err := tc.remove(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, year := range []int32{2016, 2001, 2011} {
		rows, err := nativeQueries.BooksByYear(db.Context, year)
		if err != nil || len(rows) != 0 {
			t.Fatalf("rows after delete (%d) = %#v, %v", year, rows, err)
		}
	}

}

func assertBooks(t *testing.T, rows []native.BooksByYearRow, expected map[uint64]string) {
	t.Helper()
	if len(rows) != len(expected) {
		t.Fatalf("native rows = %#v", rows)
	}
	for _, row := range rows {
		if tags, ok := expected[row.BookID]; !ok || !jsonEqual(row.Tags, tags) {
			t.Fatalf("native row = %#v", row)
		}
	}
}

func assertSQLBooks(t *testing.T, rows []sq.BooksByYearRow, expected map[uint64]string) {
	t.Helper()
	if len(rows) != len(expected) {
		t.Fatalf("database/sql rows = %#v", rows)
	}
	for _, row := range rows {
		if tags, ok := expected[row.BookID]; !ok || !jsonEqual(row.Tags, tags) {
			t.Fatalf("database/sql row = %#v", row)
		}
	}
}

func jsonEqual(left, right string) bool {
	var a, b any
	return json.Unmarshal([]byte(left), &a) == nil &&
		json.Unmarshal([]byte(right), &b) == nil && reflect.DeepEqual(a, b)
}

func TestBulkUpsertEmptyInput(t *testing.T) {
	if err := batch.BulkUpsertBooks(context.Background(), nil, "unused", nil); err != nil {
		t.Fatal(err)
	}
}
