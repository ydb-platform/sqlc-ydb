package booktest_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	sq "example.com/sqlc-ydb-examples/booktest/go/database/sql"
	native "example.com/sqlc-ydb-examples/booktest/go/native"
	"example.com/sqlc-ydb-examples/internal/testdb"
)

// TestGeneratedExample uses the native and database/sql packages in one
// sequential scenario so each runtime reads data written by the other.
func TestGeneratedExample(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../schema.sql", "DROP TABLE books;", "DROP TABLE authors;")
	ctx := db.Context
	n := native.New(db.Native)
	s := sq.New(db.SQL)

	author, err := n.CreateAuthor(ctx, native.CreateAuthorParams{AuthorID: 100, Name: "Ursula Le Guin"})
	if err != nil || author.AuthorID != 100 || author.Name != "Ursula Le Guin" {
		t.Fatalf("native CreateAuthor() = %#v, %v", author, err)
	}
	if got, err := s.GetAuthor(ctx, 100); err != nil || got.Name != author.Name {
		t.Fatalf("database/sql GetAuthor() = %#v, %v", got, err)
	}

	available := time.Now().UTC().Truncate(time.Microsecond)
	first, err := n.CreateBook(ctx, native.CreateBookParams{
		BookID: 101, AuthorID: 100, Isbn: "978-0-441-47812-5", BookType: "FICTION",
		Title: "A Wizard of Earthsea", PublicationYear: 1968, Available: available,
		Tags: `["fantasy","classic"]`,
	})
	if err != nil || first.BookID != 101 || !first.Available.Equal(available) || !sameJSON(first.Tags, `["fantasy","classic"]`) {
		t.Fatalf("native CreateBook() = %#v, %v", first, err)
	}
	if _, err := n.CreateBook(ctx, native.CreateBookParams{
		BookID: 102, AuthorID: 100, Isbn: "978-0-06-051274-3", BookType: "FICTION",
		Title: "The Dispossessed", PublicationYear: 1974, Available: available,
		Tags: `["science-fiction","politics"]`,
	}); err != nil {
		t.Fatal(err)
	}

	portable, err := s.CreateBook(ctx, sq.CreateBookParams{
		BookID: 201, AuthorID: 100, Isbn: "978-1-59853-538-9", BookType: "NONFICTION",
		Title: "Words Are My Matter", PublicationYear: 2016, Available: available,
		Tags: `["essays","portable"]`,
	})
	if err != nil || portable.BookID != 201 || !portable.Available.Equal(available) || !sameJSON(portable.Tags, `["essays","portable"]`) {
		t.Fatalf("database/sql CreateBook() = %#v, %v", portable, err)
	}
	if _, err := n.CreateBook(ctx, native.CreateBookParams{
		BookID: 202, AuthorID: 999, Isbn: "orphan", BookType: "NONFICTION",
		Title: "Unmatched author", PublicationYear: 2020, Available: available,
		Tags: `["orphan"]`,
	}); err != nil {
		t.Fatal(err)
	}

	if got, err := s.GetBook(ctx, 101); err != nil || got.Title != first.Title || !got.Available.Equal(available) {
		t.Fatalf("database/sql GetBook() = %#v, %v", got, err)
	}
	byTitle, err := n.BooksByTitleYear(ctx, native.BooksByTitleYearParams{Title: "The Dispossessed", PublicationYear: 1974})
	if err != nil || len(byTitle) != 1 || byTitle[0].BookID != 102 {
		t.Fatalf("native BooksByTitleYear() = %#v, %v", byTitle, err)
	}

	joined, err := s.BooksByTags(ctx, `["classic","orphan"]`)
	if err != nil || len(joined) != 2 {
		t.Fatalf("database/sql BooksByTags() = %#v, %v", joined, err)
	}
	for _, row := range joined {
		switch row.BookID {
		case 101:
			if row.Name == nil || *row.Name != author.Name {
				t.Fatalf("joined author = %v", row.Name)
			}
		case 202:
			if row.Name != nil {
				t.Fatalf("unmatched LEFT JOIN author = %q, want nil", *row.Name)
			}
		default:
			t.Fatalf("unexpected tag match %#v", row)
		}
	}
	if rows, err := n.BooksByTags(ctx, `["portable"]`); err != nil || len(rows) != 1 || rows[0].BookID != 201 {
		t.Fatalf("native BooksByTags() = %#v, %v", rows, err)
	}

	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WithTx(tx).UpdateBook(ctx, sq.UpdateBookParams{BookID: 102, Title: "The Dispossessed: A Novel", Tags: `["classic"]`}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got, err := n.GetBook(ctx, 102); err != nil || got.Title != "The Dispossessed: A Novel" || !sameJSON(got.Tags, `["classic"]`) {
		t.Fatalf("native GetBook() after portable update = %#v, %v", got, err)
	}
	if err := n.UpdateBookISBN(ctx, native.UpdateBookISBNParams{BookID: 201, Title: "Words Matter", Tags: `["essays"]`, Isbn: "978-new"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetBook(ctx, 201); err != nil || got.Title != "Words Matter" || got.Isbn != "978-new" {
		t.Fatalf("database/sql GetBook() after native update = %#v, %v", got, err)
	}

	if greeting, err := n.SayHello(ctx, "Ged"); err != nil || greeting.Greeting != "hello Ged" {
		t.Fatalf("native SayHello() = %#v, %v", greeting, err)
	}
	if greeting, err := s.SayHello(ctx, "Tenar"); err != nil || greeting.Greeting != "hello Tenar" {
		t.Fatalf("database/sql SayHello() = %#v, %v", greeting, err)
	}

	if err := n.DeleteAuthorBeforeYear(ctx, native.DeleteAuthorBeforeYearParams{AuthorID: 100, PublicationYear: 1970}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBook(ctx, 101); err == nil {
		t.Fatal("DeleteAuthorBeforeYear left an old book")
	}
	for _, id := range []uint64{102, 201, 202} {
		if err := s.DeleteBook(ctx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := n.GetBook(ctx, id); err == nil {
			t.Fatalf("DeleteBook(%d) left a row", id)
		}
	}
}

func sameJSON(left, right string) bool {
	var l, r any
	return json.Unmarshal([]byte(left), &l) == nil && json.Unmarshal([]byte(right), &r) == nil && reflect.DeepEqual(l, r)
}
