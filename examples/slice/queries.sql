-- name: FindRecords :many
SELECT id, label FROM records
WHERE id IN (sqlc.slice("ids"))
ORDER BY id;

-- name: ExcludeRecords :many
SELECT id, label FROM records
WHERE id NOT IN (sqlc.slice("ids"))
ORDER BY id;
