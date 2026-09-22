-- name: CreateCounter :one
DECLARE $id AS Utf8;
INSERT INTO counters (id, value, optional_value, label, enabled)
VALUES ($id, 0, NULL, 'pending'u, true)
RETURNING *;

-- name: IncrementCounter :one
DECLARE $id AS Utf8;
DECLARE $delta AS Int64;
UPDATE counters SET value = value + $delta WHERE id = $id
RETURNING *;

-- name: TransformCounter :one
DECLARE $id AS Utf8;
UPDATE counters SET value = (value + 2) * 3 - 4,
    optional_value = COALESCE(optional_value, 0l) + 1,
    label = 'done'u, enabled = (value > 10l)
WHERE id = $id
RETURNING *;

-- name: ClearOptional :one
DECLARE $id AS Utf8;
UPDATE counters SET optional_value = NULL, label = NULL WHERE id = $id
RETURNING *;

-- name: UpsertCounter :exec
DECLARE $id AS Utf8;
DECLARE $seed AS Int64;
UPSERT INTO counters (id, value, optional_value, label, enabled)
VALUES ($id, ($seed + 2) * 3, 5, 'reset'u, true);

-- name: ReadCounter :one
DECLARE $id AS Utf8;
SELECT c.* FROM counters AS c WHERE c.id = $id;

-- name: ListCounters :many
SELECT * FROM counters ORDER BY id;
