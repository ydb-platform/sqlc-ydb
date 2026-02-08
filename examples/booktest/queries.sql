-- name: GetAuthor :one
SELECT * FROM authors
WHERE author_id = $author_id LIMIT 1;

-- name: GetBook :one
SELECT * FROM books
WHERE book_id = $book_id LIMIT 1;

-- name: DeleteBook :exec
DELETE FROM books
WHERE book_id = $book_id;

-- name: BooksByTitleYear :many
SELECT * FROM books
WHERE title = $title AND year = $year;

-- name: CreateAuthor :one
INSERT INTO authors (name)
VALUES ($name)
RETURNING *;

-- name: CreateBook :one
INSERT INTO books (
    author_id,
    isbn,
    book_type,
    title,
    year,
    available,
    tags
) VALUES (
    $author_id,
    $isbn,
    $book_type,
    $title,
    $year,
    $available,
    $tags
)
RETURNING *;

-- name: UpdateBook :exec
UPDATE books
SET title = $title, tags = $tags
WHERE book_id = $book_id;

-- name: UpdateBookISBN :exec
UPDATE books
SET title = $title, tags = $tags, isbn = $isbn
WHERE book_id = $book_id;
