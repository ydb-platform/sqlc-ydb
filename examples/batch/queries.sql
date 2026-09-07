-- name: GetAuthor :one
DECLARE $author_id AS Uint64;
SELECT author_id, name, biography FROM authors
WHERE author_id = $author_id;

-- name: DeleteBookExecResult :exec
DECLARE $book_id AS Uint64;
DELETE FROM books
WHERE book_id = $book_id;

-- name: DeleteBook :exec
DECLARE $book_id AS Uint64;
DELETE FROM books
WHERE book_id = $book_id;

-- name: DeleteBookNamedFunc :exec
DECLARE $book_id AS Uint64;
DELETE FROM books
WHERE book_id = $book_id;

-- name: DeleteBookNamedSign :exec
DECLARE $book_id AS Uint64;
DELETE FROM books
WHERE book_id = $book_id;

-- name: BooksByYear :many
DECLARE $year AS Int32;
SELECT book_id, author_id, isbn, book_type, title, year, available, tags
FROM books
WHERE year = $year;

-- name: CreateAuthor :one
DECLARE $author_id AS Uint64;
DECLARE $name AS Utf8;
DECLARE $biography AS Optional<Json>;
INSERT INTO authors (author_id, name, biography)
VALUES ($author_id, $name, $biography)
RETURNING author_id, name, biography;

-- name: CreateBook :one
DECLARE $book_id AS Uint64;
DECLARE $author_id AS Uint64;
DECLARE $isbn AS Utf8;
DECLARE $book_type AS Utf8;
DECLARE $title AS Utf8;
DECLARE $year AS Int32;
DECLARE $available AS Timestamp;
DECLARE $tags AS Json;
INSERT INTO books (book_id, author_id, isbn, book_type, title, year, available, tags)
VALUES ($book_id, $author_id, $isbn, $book_type, $title, $year, $available, $tags)
RETURNING book_id, author_id, isbn, book_type, title, year, available, tags;

-- name: UpdateBook :exec
DECLARE $title AS Utf8;
DECLARE $tags AS Json;
DECLARE $book_id AS Uint64;
UPDATE books
SET title = $title, tags = $tags
WHERE book_id = $book_id;

-- name: GetBiography :one
DECLARE $author_id AS Uint64;
SELECT biography FROM authors
WHERE author_id = $author_id;
