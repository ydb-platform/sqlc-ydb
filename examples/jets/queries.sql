-- name: CountPilots :one
SELECT COUNT(*) AS pilot_count FROM pilots;

-- name: ListPilots :many
SELECT id, name FROM pilots ORDER BY id LIMIT 5;

-- name: DeletePilot :exec
DELETE FROM pilots WHERE id = $pilot_id;
