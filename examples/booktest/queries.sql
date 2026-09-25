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

-- name: RemoveBookTag :exec
DECLARE $book_id AS Uint64;
DECLARE $tag AS String;
UPDATE books
SET tags = UNWRAP(Yson::SerializeJson(Json::From(ListFilter(
    Yson::ConvertToStringList(tags),
    ($item) -> ($item NOT IN AsList($tag))
))))
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

-- name: ListAuthorsWithRecentBooks :many
SELECT a.author_id, a.name
FROM authors AS a
WHERE a.author_id IN (
    SELECT b.author_id FROM books AS b WHERE b.publication_year >= $since_year
)
ORDER BY a.author_id;

-- name: ListBooksWithRecentEditions :many
DECLARE $since_year AS Int32;
SELECT b.book_id, b.author_id, b.isbn, b.book_type, b.title, b.publication_year, b.available, b.tags
FROM books AS b
WHERE (b.author_id, b.book_type) IN (
    SELECT (recent.author_id, recent.book_type)
    FROM books AS recent
    WHERE recent.publication_year >= $since_year
)
ORDER BY b.book_id;

-- name: DeleteBooksByAuthorName :exec
DELETE FROM books
WHERE author_id IN (SELECT author_id FROM authors WHERE name = $author_name);

-- name: DeleteAuthorWithBooks :exec
DECLARE $author_id AS Uint64;
DELETE FROM books WHERE author_id = $author_id;
DELETE FROM authors WHERE author_id = $author_id;

-- name: UpdateAuthorAndListBooks :many
DECLARE $author_id AS Uint64;
DECLARE $name AS Utf8;
UPDATE authors SET name = $name WHERE author_id = $author_id;
SELECT book_id, title FROM books WHERE author_id = $author_id ORDER BY book_id;

-- name: SelectAuthorAndDeleteBooks :one
DECLARE $author_id AS Uint64;
SELECT author_id, name FROM authors WHERE author_id = $author_id;
DELETE FROM books WHERE author_id = $author_id;

-- name: ListAuthorBookTitles :many
DECLARE $since_year AS Int32;
$recent = (SELECT author_id, title FROM books WHERE publication_year >= $since_year);
$grouped = (
    SELECT author_id, AGGREGATE_LIST(title, 100u) AS titles
    FROM $recent
    GROUP BY author_id
);
SELECT a.author_id, a.name, Yson::SerializeJson(Json::From(g.titles)) AS titles_json
FROM (SELECT author_id, name FROM authors) AS a
JOIN $grouped AS g ON a.author_id = g.author_id
ORDER BY a.author_id;

-- name: InspectBookText :one
DECLARE $text AS String;
SELECT
    String::Base32Encode($text) AS base32,
    Unicode::IsAlpha("Book"u) AS alphabetic,
    Url::GetHost("https://example.org/books") AS host,
    Math::Sqrt(9.0) AS square_root,
    Yson::IsString(Yson::From($text)) AS yson_string,
    Pire::Grep("book")($text) AS pattern_found;
