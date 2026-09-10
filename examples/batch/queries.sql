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
