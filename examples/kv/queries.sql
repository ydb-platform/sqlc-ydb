-- name: Get :one
SELECT key, value FROM kv
WHERE key = $key LIMIT 1;

-- name: List :many
SELECT key, value FROM kv
ORDER BY key;

-- name: Set :exec
INSERT INTO kv (key, value)
VALUES ($key, $value);

-- name: Delete :exec
DELETE FROM kv
WHERE key = $key;
