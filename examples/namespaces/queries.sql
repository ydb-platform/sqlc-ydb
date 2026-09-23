-- name: UpsertPrimaryUser :exec
PRAGMA TablePathPrefix("/local/sqlc_namespaces/a");
DECLARE $id AS Uint64;
DECLARE $name AS Utf8;
UPSERT INTO users (id, name) VALUES ($id, $name);

-- name: UpsertSecondaryUser :exec
PRAGMA TablePathPrefix("/local/sqlc_namespaces/b");
DECLARE $id AS Uint64;
DECLARE $name AS Utf8;
UPSERT INTO users (id, name) VALUES ($id, $name);

-- name: GetPrimaryUser :one
PRAGMA TablePathPrefix("/local/sqlc_namespaces/a");
SELECT users.* FROM users WHERE id = $id;

-- name: GetSecondaryUser :one
PRAGMA TablePathPrefix("/local/sqlc_namespaces/b");
DECLARE $id AS Uint64;
SELECT users.* FROM users WHERE id = $id;

-- name: FindPrimaryUsersByName :many
PRAGMA TablePathPrefix("/local/sqlc_namespaces/a");
SELECT u.* FROM users VIEW by_name AS u WHERE u.name = $name ORDER BY u.id;

-- name: CompareUserNames :many
PRAGMA TablePathPrefix("/local/sqlc_namespaces/a");
SELECT a.id AS id, a.name AS primary_name, b.name AS secondary_name
FROM users AS a
LEFT JOIN `/local/sqlc_namespaces/b/users` AS b ON a.id = b.id
ORDER BY a.id;
