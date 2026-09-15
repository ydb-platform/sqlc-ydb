package batch_test

import (
	"context"
	"encoding/json"
	"path"
	"reflect"
	"testing"
	"time"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	batch "example.com/sqlc-ydb-examples/batch/go"
	sq "example.com/sqlc-ydb-examples/batch/go/database/sql"
	native "example.com/sqlc-ydb-examples/batch/go/native"
)

func TestBulkUpsertBooks(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/batch/schema.sql", "DROP TABLE books;", "DROP TABLE authors;")

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

func TestCreateBooksFromStructList(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/batch/schema.sql", "DROP TABLE books;", "DROP TABLE authors;")
	nq, sqlq := native.New(db.Native), sq.New(db.SQL)
	available := time.Date(2026, time.January, 2, 3, 4, 5, 123456000, time.UTC)
	for _, books := range [][]native.CreateBooksBooksItem{nil, {}} {
		if err := nq.CreateBooks(db.Context, books); err != nil {
			t.Fatalf("native empty: %v", err)
		}
	}
	for _, books := range [][]sq.CreateBooksBooksItem{nil, {}} {
		if err := sqlq.CreateBooks(db.Context, books); err != nil {
			t.Fatalf("database/sql empty: %v", err)
		}
	}
	nativeBooks := []native.CreateBooksBooksItem{
		{BookID: ^uint64(0), AuthorID: 1, Isbn: "high", BookType: "FICTION", Title: "Unicode ☀", Year: 2026, Available: available, Tags: `{"kind":"native"}`},
		{BookID: 1, AuthorID: 1, Isbn: "one", BookType: "FICTION", Title: "Second", Year: 2026, Available: available, Tags: `[]`},
	}
	if err := nq.CreateBooks(db.Context, nativeBooks); err != nil {
		t.Fatal(err)
	}
	sqlBooks := []sq.CreateBooksBooksItem{
		{BookID: 2, AuthorID: 1, Isbn: "two", BookType: "REFERENCE", Title: "SQL batch", Year: 2026, Available: available, Tags: `["sql"]`},
		{BookID: 3, AuthorID: 1, Isbn: "three", BookType: "REFERENCE", Title: "Fourth", Year: 2026, Available: available, Tags: `{}`},
	}
	if err := sqlq.CreateBooks(db.Context, sqlBooks); err != nil {
		t.Fatal(err)
	}
	rows, err := nq.BooksByYear(db.Context, 2026)
	if err != nil {
		t.Fatal(err)
	}
	assertBooks(t, rows, map[uint64]string{^uint64(0): `{"kind":"native"}`, 1: `[]`, 2: `["sql"]`, 3: `{}`})
	for _, row := range rows {
		if !row.Available.Equal(available) {
			t.Fatalf("timestamp lost precision: %v", row.Available)
		}
	}
	if err := nq.CreateBooks(db.Context, nativeBooks[:1]); err == nil {
		t.Fatal("native duplicate INSERT must fail")
	}
	if err := sqlq.CreateBooks(db.Context, sqlBooks[:1]); err == nil {
		t.Fatal("database/sql duplicate INSERT must fail")
	}
	cancelled, cancel := context.WithCancel(db.Context)
	cancel()
	if err := nq.CreateBooks(cancelled, nativeBooks); err == nil {
		t.Fatal("native cancelled INSERT succeeded")
	}
	if err := sqlq.CreateBooks(cancelled, sqlBooks); err == nil {
		t.Fatal("database/sql cancelled INSERT succeeded")
	}
}
