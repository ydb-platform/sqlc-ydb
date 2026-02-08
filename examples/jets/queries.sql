-- name: CountPilots :one
SELECT COUNT(*) FROM pilots;

-- name: ListPilots :many
SELECT * FROM pilots
LIMIT 5;

-- name: DeletePilot :exec
DELETE FROM pilots
WHERE id = $id;

-- name: GetPilot :one
SELECT * FROM pilots
WHERE id = $id LIMIT 1;

-- name: CreatePilot :one
INSERT INTO pilots (name)
VALUES ($name)
RETURNING *;
