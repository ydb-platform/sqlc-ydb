-- name: GetAuthor :one
SELECT id, name, bio FROM authors WHERE id = $author_id;

-- name: ListAuthors :many
SELECT id, name, bio FROM authors ORDER BY name;

-- name: ListAuthorsPage :many
DECLARE $page_size AS Int;
DECLARE $offset AS Uint32;
SELECT id, name, bio FROM authors ORDER BY id LIMIT $page_size OFFSET $offset;

-- name: GetAuthorName :one
SELECT name FROM authors WHERE id = $author_id;

-- name: CreateAuthor :one
INSERT INTO `authors` (`id`, `name`, `bio`)
VALUES ($author_id, $author_name, $biography)
RETURNING `id`, `name`, `bio`;

-- name: UpsertAuthor :exec
UPSERT INTO authors (id, name, bio)
VALUES ($author_id, $author_name, $biography);

-- name: DeleteAuthor :exec
DELETE FROM authors WHERE id = $author_id;

-- name: FindAuthorsByName :many
SELECT a.* FROM authors VIEW by_name AS a
WHERE a.name = $name ORDER BY a.id;

-- name: FindAuthorsByNameCovering :many
DECLARE $name AS Utf8;
SELECT * FROM authors VIEW by_name_covering WHERE name = $name ORDER BY id;

-- name: FindAuthorsByNamePrefix :many
DECLARE $prefix AS Utf8;
SELECT id, name, bio, bio IS NOT NULL AS has_bio
FROM authors
WHERE name LIKE $prefix || "%"u
ORDER BY id;

-- name: GetAuthorStatistics :one
SELECT
    COUNT(*) AS total,
    COUNT_IF(bio IS NOT NULL) AS with_bio,
    COUNT_IF(bio != ""u) AS with_nonempty_bio,
    CAST(COUNT(*) AS Bool)
FROM authors;

-- name: GetAuthorExportMetadata :one
DECLARE $author_id AS Uint64;
SELECT
    id,
    CAST(CurrentUtcDate() AS String) AS export_date,
    CAST(CurrentUtcDatetime() AS String) AS export_datetime,
    CurrentUtcTimestamp() AS export_timestamp,
    CAST(CurrentUtcTimestamp() AS String) AS export_timestamp_text,
    CAST(CurrentUtcTimestamp() AS Uint64) AS export_timestamp_micros,
    COALESCE(CAST(id AS Uint32), 0),
    CAST('{"source":"authors"}' AS Json) AS export_metadata
FROM authors
WHERE id = $author_id;

-- name: EchoAuthorIDText :one
SELECT $author_id AS author_id_text;
