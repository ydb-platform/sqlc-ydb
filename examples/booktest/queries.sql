-- name: GetAuthor :one
SELECT author_id, name
FROM authors
WHERE author_id = $author_id;

-- name: GetBook :one
SELECT book_id, author_id, isbn, book_type, title, publication_year, available, tags
FROM books
WHERE book_id = $book_id;

-- name: DeleteBook :exec
DELETE FROM books
WHERE book_id = $book_id;

-- name: BooksByTitleYear :many
SELECT book_id, author_id, isbn, book_type, title, publication_year, available, tags
FROM books
WHERE title = $title AND publication_year = $publication_year;

-- name: BooksByTags :many
DECLARE $tags AS Json;
SELECT
    b.book_id,
    b.title,
    a.name,
    b.isbn,
    b.tags
FROM books AS b
LEFT JOIN authors AS a ON b.author_id = a.author_id
WHERE NOT SetIsDisjoint(
    ToSet(Yson::ConvertToStringList(b.tags)),
    Yson::ConvertToStringList($tags)
);

-- name: CreateAuthor :one
INSERT INTO authors (author_id, name)
VALUES ($author_id, $name)
RETURNING author_id, name;

-- name: CreateBook :one
INSERT INTO books (
    book_id,
    author_id,
    isbn,
    book_type,
    title,
    publication_year,
    available,
    tags
) VALUES (
    $book_id,
    $author_id,
    $isbn,
    $book_type,
    $title,
    $publication_year,
    $available,
    $tags
)
RETURNING book_id, author_id, isbn, book_type, title, publication_year, available, tags;

-- name: UpdateBook :exec
UPDATE books
SET title = $title, tags = $tags
WHERE book_id = $book_id;

-- name: UpdateBookISBN :exec
UPDATE books
SET title = $title, tags = $tags, isbn = $isbn
WHERE book_id = $book_id;

-- name: DeleteAuthorBeforeYear :exec
DELETE FROM books
WHERE publication_year < $publication_year AND author_id = $author_id;

-- name: SayHello :one
SELECT "hello "u || $name AS greeting;
