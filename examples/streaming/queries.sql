-- name: UpsertDevice :exec
DECLARE $id AS Uint64;
DECLARE $name AS Utf8?;
UPSERT INTO streaming_devices (id, name) VALUES ($id, $name);

-- name: VisitDevices :each
SELECT id, name
FROM streaming_devices
WHERE id BETWEEN $min_id AND $max_id
ORDER BY id;

-- name: VisitAllDevices :each
SELECT id, name FROM streaming_devices ORDER BY id;
