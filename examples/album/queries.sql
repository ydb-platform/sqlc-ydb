-- name: GetAuthor :one
SELECT * FROM authors
WHERE id = $id LIMIT 1;

-- name: ListAuthors :many
SELECT * FROM authors
ORDER BY name;

-- name: CreateAuthor :one
INSERT INTO authors (name)
VALUES ($name)
RETURNING *;

-- name: GetAlbum :one
SELECT * FROM albums
WHERE id = $id LIMIT 1;

-- name: ListAlbumsByAuthor :many
SELECT * FROM albums
WHERE author_id = $author_id
ORDER BY title;

-- name: CreateAlbum :one
INSERT INTO albums (title, author_id)
VALUES ($title, $author_id)
RETURNING *;

-- name: DeleteAlbum :exec
DELETE FROM albums
WHERE id = $id;
