-- name: GetAuthor :one
SELECT id, name, bio FROM authors WHERE id = $author_id;

-- name: ListAuthors :many
SELECT id, name, bio FROM authors ORDER BY name;

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
