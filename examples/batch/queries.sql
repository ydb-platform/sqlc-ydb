-- name: GetAuthor :one
SELECT author_id, name, biography FROM authors
WHERE author_id = $author_id;

-- name: DeleteBookExecResult :exec
DELETE FROM books
WHERE book_id = $book_id;

-- name: DeleteBook :exec
DELETE FROM books
WHERE book_id = $book_id;

-- name: DeleteBookNamedFunc :exec
DELETE FROM books
WHERE book_id = $book_id;

-- name: DeleteBookNamedSign :exec
DELETE FROM books
WHERE book_id = $book_id;

-- name: BooksByYear :many
SELECT book_id, author_id, isbn, book_type, title, year, available, tags
FROM books
WHERE year = $year;

-- name: CreateAuthor :one
INSERT INTO authors (author_id, name, biography)
VALUES ($author_id, $name, $biography)
RETURNING author_id, name, biography;

-- name: CreateBook :one
INSERT INTO books (book_id, author_id, isbn, book_type, title, year, available, tags)
VALUES ($book_id, $author_id, $isbn, $book_type, $title, $year, $available, $tags)
RETURNING book_id, author_id, isbn, book_type, title, year, available, tags;

-- name: UpdateBook :exec
UPDATE books
SET title = $title, tags = $tags
WHERE book_id = $book_id;

-- name: GetBiography :one
SELECT biography FROM authors
WHERE author_id = $author_id;

-- name: CreateBooks :exec
DECLARE $books AS List<Struct<
    book_id: Uint64,
    author_id: Uint64,
    isbn: Utf8,
    book_type: Utf8,
    title: Utf8,
    year: Int32,
    available: Timestamp,
    tags: Json
>>;
INSERT INTO books (
    book_id, author_id, isbn, book_type, title, year, available, tags
)
SELECT
    book_id, author_id, isbn, book_type, title, year, available, tags
FROM AS_TABLE($books);

-- name: CreateAuthors :exec
DECLARE $authors AS List<Struct<name: Utf8, author_id: Uint64,>>;
INSERT INTO authors SELECT a.*, NULL AS biography
FROM AS_TABLE($authors) AS a;

-- name: UpsertAuthors :exec
DECLARE $authors AS List<Struct<name: Utf8, author_id: Uint64,>>;
UPSERT INTO authors SELECT * FROM AS_TABLE($authors);
