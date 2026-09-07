-- name: GetAuthor :one
DECLARE $author_id AS Uint64;
SELECT id, name, bio FROM authors WHERE id = $author_id;

-- name: ListAuthors :many
SELECT id, name, bio FROM authors ORDER BY name;

-- name: GetAuthorName :one
DECLARE $author_id AS Uint64;
SELECT name FROM authors WHERE id = $author_id;

-- name: CreateAuthor :one
DECLARE $author_id AS Uint64;
DECLARE $author_name AS Utf8;
DECLARE $biography AS Optional<Utf8>;
INSERT INTO authors (id, name, bio)
VALUES ($author_id, $author_name, $biography)
RETURNING id, name, bio;

-- name: UpsertAuthor :exec
DECLARE $author_id AS Uint64;
DECLARE $author_name AS Utf8;
DECLARE $biography AS Optional<Utf8>;
UPSERT INTO authors (id, name, bio)
VALUES ($author_id, $author_name, $biography);

-- name: DeleteAuthor :exec
DECLARE $author_id AS Uint64;
DELETE FROM authors WHERE id = $author_id;
