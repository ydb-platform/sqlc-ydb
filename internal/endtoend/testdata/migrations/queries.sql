-- name: GetAuthor :one
DECLARE $id AS Uint64;
SELECT * FROM authors WHERE id = $id;
