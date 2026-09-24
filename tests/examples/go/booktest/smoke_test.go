package booktest_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/booktest/go/database/sql"
	native "example.com/sqlc-ydb-examples/booktest/go/native"
)

// TestGeneratedExample uses the native and database/sql packages in one
// sequential scenario so each runtime reads data written by the other.
func TestGeneratedExample(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/booktest/schema.sql", "DROP TABLE books;", "DROP TABLE authors;")
	ctx := db.Context
	n := native.New(db.Native)
	s := sq.New(db.SQL)

	author, err := n.CreateAuthor(ctx, native.CreateAuthorParams{AuthorID: 100, Name: "Ursula Le Guin"})
	require.NoError(t, err)
	require.Equal(t, uint64(100), author.AuthorID)
	require.Equal(t, "Ursula Le Guin", author.Name)
	if got, err := s.GetAuthor(ctx, 100); err != nil || got.Name != author.Name {
		require.FailNow(t, fmt.Sprintf("database/sql GetAuthor() = %#v, %v", got, err))
	}

	available := time.Now().UTC().Truncate(time.Microsecond)
	first, err := n.CreateBook(ctx, native.CreateBookParams{
		BookID: 101, AuthorID: 100, Isbn: "978-0-441-47812-5", BookType: "FICTION",
		Title: "A Wizard of Earthsea", PublicationYear: 1968, Available: available,
		Tags: `["fantasy","classic"]`,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(101), first.BookID)
	require.True(t, first.Available.Equal(available), "available = %v, want %v", first.Available, available)
	require.True(t, sameJSON(first.Tags, `["fantasy","classic"]`), "tags = %s", first.Tags)
	if _, err := n.CreateBook(ctx, native.CreateBookParams{
		BookID: 102, AuthorID: 100, Isbn: "978-0-06-051274-3", BookType: "FICTION",
		Title: "The Dispossessed", PublicationYear: 1974, Available: available,
		Tags: `["science-fiction","politics"]`,
	}); err != nil {
		require.NoError(t, err)
	}

	portable, err := s.CreateBook(ctx, sq.CreateBookParams{
		BookID: 201, AuthorID: 100, Isbn: "978-1-59853-538-9", BookType: "NONFICTION",
		Title: "Words Are My Matter", PublicationYear: 2016, Available: available,
		Tags: `["essays","portable"]`,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(201), portable.BookID)
	require.True(t, portable.Available.Equal(available), "available = %v, want %v", portable.Available, available)
	require.True(t, sameJSON(portable.Tags, `["essays","portable"]`), "tags = %s", portable.Tags)
	if _, err := n.CreateBook(ctx, native.CreateBookParams{
		BookID: 202, AuthorID: 999, Isbn: "orphan", BookType: "NONFICTION",
		Title: "Unmatched author", PublicationYear: 2020, Available: available,
		Tags: `["orphan"]`,
	}); err != nil {
		require.NoError(t, err)
	}

	if got, err := s.GetBook(ctx, 101); err != nil || got.Title != first.Title || !got.Available.Equal(available) {
		require.FailNow(t, fmt.Sprintf("database/sql GetBook() = %#v, %v", got, err))
	}
	byTitle, err := n.BooksByTitleYear(ctx, native.BooksByTitleYearParams{Title: "The Dispossessed", PublicationYear: 1974})
	require.NoError(t, err)
	require.Len(t, byTitle, 1)
	require.Equal(t, uint64(102), byTitle[0].BookID)

	joined, err := s.BooksByTags(ctx, `["classic","orphan"]`)
	require.NoError(t, err)
	require.Len(t, joined, 2)
	for _, row := range joined {
		switch row.BookID {
		case 101:
			require.False(t, row.Name == nil || *row.Name != author.Name, "joined author = %v", row.Name)
		case 202:
			require.Nil(t, row.Name, "unmatched LEFT JOIN author")
		default:
			require.FailNow(t, fmt.Sprintf("unexpected tag match %#v", row))
		}
	}
	if rows, err := n.BooksByTags(ctx, `["portable"]`); err != nil || len(rows) != 1 || rows[0].BookID != 201 {
		require.FailNow(t, fmt.Sprintf("native BooksByTags() = %#v, %v", rows, err))
	}

	if rows, err := n.ListAuthorsWithRecentBooks(ctx, 2000); err != nil || len(rows) != 1 || rows[0].AuthorID != 100 || rows[0].Name != author.Name {
		require.FailNow(t, fmt.Sprintf("native ListAuthorsWithRecentBooks() = %#v, %v", rows, err))
	}
	if rows, err := s.ListAuthorsWithRecentBooks(ctx, 2000); err != nil || len(rows) != 1 || rows[0].AuthorID != 100 || rows[0].Name != author.Name {
		require.FailNow(t, fmt.Sprintf("database/sql ListAuthorsWithRecentBooks() = %#v, %v", rows, err))
	}
	if rows, err := n.ListAuthorsWithRecentBooks(ctx, 2021); err != nil || len(rows) != 0 {
		require.FailNow(t, fmt.Sprintf("native empty author subquery = %#v, %v", rows, err))
	}
	if rows, err := s.ListAuthorsWithRecentBooks(ctx, 2021); err != nil || len(rows) != 0 {
		require.FailNow(t, fmt.Sprintf("database/sql empty author subquery = %#v, %v", rows, err))
	}
	if rows, err := n.ListBooksWithRecentEditions(ctx, 2000); err != nil || len(rows) != 2 || rows[0].BookID != 201 || rows[1].BookID != 202 {
		require.FailNow(t, fmt.Sprintf("native tuple subquery must compare author and type: %#v, %v", rows, err))
	}
	if rows, err := s.ListBooksWithRecentEditions(ctx, 2000); err != nil || len(rows) != 2 || rows[0].BookID != 201 || rows[1].BookID != 202 {
		require.FailNow(t, fmt.Sprintf("database/sql tuple subquery must compare author and type: %#v, %v", rows, err))
	}
	if rows, err := n.ListBooksWithRecentEditions(ctx, 2021); err != nil || len(rows) != 0 {
		require.FailNow(t, fmt.Sprintf("native empty tuple subquery = %#v, %v", rows, err))
	}
	if rows, err := s.ListBooksWithRecentEditions(ctx, 2021); err != nil || len(rows) != 0 {
		require.FailNow(t, fmt.Sprintf("database/sql empty tuple subquery = %#v, %v", rows, err))
	}
	if rows, err := n.ListAuthorBookTitles(ctx, 1970); err != nil || len(rows) != 1 || rows[0].AuthorID != 100 || rows[0].Name != author.Name || !sameJSONStrings(rows[0].TitlesJson, "The Dispossessed", "Words Are My Matter") {
		require.FailNow(t, fmt.Sprintf("native grouped book titles = %#v, %v", rows, err))
	}
	if rows, err := s.ListAuthorBookTitles(ctx, 2000); err != nil || len(rows) != 1 || rows[0].AuthorID != 100 || rows[0].Name != author.Name || !sameJSONStrings(rows[0].TitlesJson, "Words Are My Matter") {
		require.FailNow(t, fmt.Sprintf("database/sql grouped book titles = %#v, %v", rows, err))
	}
	if rows, err := n.ListAuthorBookTitles(ctx, 2021); err != nil || len(rows) != 0 {
		require.FailNow(t, fmt.Sprintf("native empty grouped book titles = %#v, %v", rows, err))
	}
	if rows, err := s.ListAuthorBookTitles(ctx, 2021); err != nil || len(rows) != 0 {
		require.FailNow(t, fmt.Sprintf("database/sql empty grouped book titles = %#v, %v", rows, err))
	}

	tx, err := db.SQL.BeginTx(ctx, nil)
	require.NoError(t, err)
	if err := s.WithTx(tx).UpdateBook(ctx, sq.UpdateBookParams{BookID: 102, Title: "The Dispossessed: A Novel", Tags: `["classic"]`}); err != nil {
		_ = tx.Rollback()
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	if got, err := n.GetBook(ctx, 102); err != nil || got.Title != "The Dispossessed: A Novel" || !sameJSON(got.Tags, `["classic"]`) {
		require.FailNow(t, fmt.Sprintf("native GetBook() after portable update = %#v, %v", got, err))
	}
	require.NoError(t, n.UpdateBookISBN(ctx, native.UpdateBookISBNParams{BookID: 201, Title: "Words Matter", Tags: `["essays"]`, Isbn: "978-new"}))
	if got, err := s.GetBook(ctx, 201); err != nil || got.Title != "Words Matter" || got.Isbn != "978-new" {
		require.FailNow(t, fmt.Sprintf("database/sql GetBook() after native update = %#v, %v", got, err))
	}

	if greeting, err := n.SayHello(ctx, "Ged"); err != nil || greeting.Greeting != "hello Ged" {
		require.FailNow(t, fmt.Sprintf("native SayHello() = %#v, %v", greeting, err))
	}
	if greeting, err := s.SayHello(ctx, "Tenar"); err != nil || greeting.Greeting != "hello Tenar" {
		require.FailNow(t, fmt.Sprintf("database/sql SayHello() = %#v, %v", greeting, err))
	}
	if got, err := n.InspectBookText(ctx, []byte("book")); err != nil || string(got.Base32) != "MJXW62Y=" || !got.Alphabetic || got.Host == nil || string(*got.Host) != "example.org" || got.SquareRoot != 3 || !got.YsonString || !got.PatternFound {
		require.FailNow(t, fmt.Sprintf("native InspectBookText() = %#v, %v", got, err))
	}
	if got, err := s.InspectBookText(ctx, []byte("book")); err != nil || string(got.Base32) != "MJXW62Y=" || !got.Alphabetic || got.Host == nil || string(*got.Host) != "example.org" || got.SquareRoot != 3 || !got.YsonString || !got.PatternFound {
		require.FailNow(t, fmt.Sprintf("database/sql InspectBookText() = %#v, %v", got, err))
	}

	require.NoError(t, n.DeleteAuthorBeforeYear(ctx, native.DeleteAuthorBeforeYearParams{AuthorID: 100, PublicationYear: 1970}))
	if _, err := s.GetBook(ctx, 101); err == nil {
		require.Error(t, err, "DeleteAuthorBeforeYear left an old book")
	}
	require.NoError(t, s.DeleteBook(ctx, 102))
	if _, err := n.GetBook(ctx, 102); err == nil {
		require.Error(t, err, "DeleteBook left an existing row")
	}
	require.NoError(t, n.DeleteBooksByAuthorName(ctx, "absent"))
	require.NoError(t, n.DeleteBooksByAuthorName(ctx, author.Name))
	for _, id := range []uint64{102, 201} {
		if _, err := s.GetBook(ctx, id); err == nil {
			require.Error(t, err, "native DeleteBooksByAuthorName left book %d", id)
		}
	}
	if _, err := n.GetBook(ctx, 202); err != nil {
		require.NoError(t, err, "DeleteBooksByAuthorName removed unmatched author: %v", err)
	}
	if _, err := s.CreateAuthor(ctx, sq.CreateAuthorParams{AuthorID: 999, Name: "Second author"}); err != nil {
		require.NoError(t, err)
	}
	require.NoError(t, s.DeleteBooksByAuthorName(ctx, "Second author"))
	if _, err := n.GetBook(ctx, 202); err == nil {
		require.Error(t, err, "database/sql DeleteBooksByAuthorName left book 202")
	}
	for _, id := range []uint64{102, 201, 202} {
		require.NoError(t, s.DeleteBook(ctx, id))
		if _, err := n.GetBook(ctx, id); err == nil {
			require.Error(t, err, "DeleteBook(%d) left a row", id)
		}
	}
	for _, authorID := range []uint64{100, 999} {
		if _, err := s.CreateBook(ctx, sq.CreateBookParams{
			BookID: authorID + 1000, AuthorID: authorID, Isbn: "cleanup", BookType: "FICTION",
			Title: "Author cleanup", PublicationYear: 2026, Available: available, Tags: `[]`,
		}); err != nil {
			require.NoError(t, err)
		}
	}
	require.NoError(t, n.DeleteAuthorWithBooks(ctx, 100))
	if _, err := s.GetBook(ctx, 1100); err == nil {
		require.Error(t, err, "native DeleteAuthorWithBooks left the book")
	}
	if _, err := s.GetAuthor(ctx, 100); err == nil {
		require.Error(t, err, "native DeleteAuthorWithBooks left the author")
	}
	if _, err := n.GetBook(ctx, 1999); err != nil {
		require.NoError(t, err, "DeleteAuthorWithBooks removed another author's book: %v", err)
	}
	if _, err := n.GetAuthor(ctx, 999); err != nil {
		require.NoError(t, err, "DeleteAuthorWithBooks removed another author: %v", err)
	}
	require.NoError(t, s.DeleteAuthorWithBooks(ctx, 999))
	if _, err := n.GetBook(ctx, 1999); err == nil {
		require.Error(t, err, "database/sql DeleteAuthorWithBooks left the book")
	}
	if _, err := n.GetAuthor(ctx, 999); err == nil {
		require.Error(t, err, "database/sql DeleteAuthorWithBooks left the author")
	}
}

func sameJSON(left, right string) bool {
	var l, r any
	return json.Unmarshal([]byte(left), &l) == nil && json.Unmarshal([]byte(right), &r) == nil && reflect.DeepEqual(l, r)
}

func sameJSONStrings(value *string, want ...string) bool {
	if value == nil {
		return false
	}
	var got []string
	if err := json.Unmarshal([]byte(*value), &got); err != nil {
		return false
	}
	sort.Strings(got)
	sort.Strings(want)
	return reflect.DeepEqual(got, want)
}

func TestGeneratedMixedScripts(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/booktest/schema.sql", "DROP TABLE books;", "DROP TABLE authors;")
	ctx := db.Context
	n := native.New(db.Native)
	s := sq.New(db.SQL)
	for _, authorID := range []uint64{700, 800} {
		if _, err := n.CreateAuthor(ctx, native.CreateAuthorParams{AuthorID: authorID, Name: "Before"}); err != nil {
			require.NoError(t, err)
		}
		for _, bookID := range []uint64{authorID + 1, authorID + 2} {
			if _, err := s.CreateBook(ctx, sq.CreateBookParams{BookID: bookID, AuthorID: authorID, Isbn: "script", BookType: "FICTION", Title: "Book", PublicationYear: 2026, Available: time.Now().UTC(), Tags: `[]`}); err != nil {
				require.NoError(t, err)
			}
		}
	}
	rows, err := n.UpdateAuthorAndListBooks(ctx, native.UpdateAuthorAndListBooksParams{AuthorID: 700, Name: "Native"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, uint64(701), rows[0].BookID)
	require.Equal(t, uint64(702), rows[1].BookID)
	require.Equal(t, "Book", rows[0].Title)
	if author, err := s.GetAuthor(ctx, 700); err != nil || author.Name != "Native" {
		require.FailNow(t, fmt.Sprintf("native script write: %#v, %v", author, err))
	}
	sqlRows, err := s.UpdateAuthorAndListBooks(ctx, sq.UpdateAuthorAndListBooksParams{AuthorID: 800, Name: "SQL"})
	require.NoError(t, err)
	require.Len(t, sqlRows, 2)
	require.Equal(t, uint64(801), sqlRows[0].BookID)
	require.Equal(t, uint64(802), sqlRows[1].BookID)
	require.Equal(t, "Book", sqlRows[0].Title)
	if author, err := n.SelectAuthorAndDeleteBooks(ctx, 700); err != nil || author.AuthorID != 700 || author.Name != "Native" {
		require.FailNow(t, fmt.Sprintf("native SelectAuthorAndDeleteBooks: %#v, %v", author, err))
	}
	for _, bookID := range []uint64{701, 702} {
		if _, err := s.GetBook(ctx, bookID); err == nil {
			require.Error(t, err, "native script left book %d", bookID)
		}
	}
	if _, err := s.GetBook(ctx, 801); err != nil {
		require.NoError(t, err, "native script removed unrelated book: %v", err)
	}
	if author, err := s.SelectAuthorAndDeleteBooks(ctx, 800); err != nil || author.AuthorID != 800 || author.Name != "SQL" {
		require.FailNow(t, fmt.Sprintf("database/sql SelectAuthorAndDeleteBooks: %#v, %v", author, err))
	}
	for _, bookID := range []uint64{801, 802} {
		if _, err := n.GetBook(ctx, bookID); err == nil {
			require.Error(t, err, "database/sql script left book %d", bookID)
		}
	}
	for _, authorID := range []uint64{700, 800} {
		if _, err := n.GetAuthor(ctx, authorID); err != nil {
			require.NoError(t, err, "script removed author %d: %v", authorID, err)
		}
	}
}
