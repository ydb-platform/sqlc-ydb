-- name: GetAuthor :one
DECLARE $author_id AS Uint64;
SELECT author_id, name
FROM authors
WHERE author_id = $author_id;

-- name: GetBook :one
DECLARE $book_id AS Uint64;
SELECT book_id, author_id, isbn, book_type, title, publication_year, available, tags
FROM books
WHERE book_id = $book_id;

-- name: DeleteBook :exec
DECLARE $book_id AS Uint64;
DELETE FROM books
WHERE book_id = $book_id;

-- name: BooksByTitleYear :many
DECLARE $title AS Utf8;
DECLARE $publication_year AS Int32;
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
DECLARE $author_id AS Uint64;
DECLARE $name AS Utf8;
INSERT INTO authors (author_id, name)
VALUES ($author_id, $name)
RETURNING author_id, name;

-- name: CreateBook :one
DECLARE $book_id AS Uint64;
DECLARE $author_id AS Uint64;
DECLARE $isbn AS Utf8;
DECLARE $book_type AS Utf8;
DECLARE $title AS Utf8;
DECLARE $publication_year AS Int32;
DECLARE $available AS Timestamp;
DECLARE $tags AS Json;
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
DECLARE $book_id AS Uint64;
DECLARE $title AS Utf8;
DECLARE $tags AS Json;
UPDATE books
SET title = $title, tags = $tags
WHERE book_id = $book_id;

-- name: UpdateBookISBN :exec
DECLARE $book_id AS Uint64;
DECLARE $title AS Utf8;
DECLARE $tags AS Json;
DECLARE $isbn AS Utf8;
UPDATE books
SET title = $title, tags = $tags, isbn = $isbn
WHERE book_id = $book_id;

-- name: DeleteAuthorBeforeYear :exec
DECLARE $author_id AS Uint64;
DECLARE $publication_year AS Int32;
DELETE FROM books
WHERE publication_year < $publication_year AND author_id = $author_id;

-- name: SayHello :one
DECLARE $name AS Utf8;
SELECT "hello "u || $name AS greeting;
