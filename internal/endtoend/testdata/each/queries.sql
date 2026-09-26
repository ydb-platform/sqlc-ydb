-- name: VisitDevices :each
SELECT id, name
FROM devices
WHERE id BETWEEN $min_id AND $max_id
ORDER BY id;

-- name: VisitNamedDevices :each
SELECT id, name
FROM devices
WHERE name BETWEEN $min_name AND $max_name
ORDER BY id;

-- name: VisitFrom :each
DECLARE $min_id AS Uint64;
SELECT id, name FROM devices WHERE id >= $min_id ORDER BY id;

-- name: VisitAll :each
SELECT id, name FROM devices ORDER BY id;

-- name: PutDevices :exec
DECLARE $devices AS List<Struct<id: Uint64, name: Optional<Utf8>>>;
UPSERT INTO devices (id, name) SELECT id, name FROM AS_TABLE($devices);
