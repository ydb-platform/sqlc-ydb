-- name: InsertRecords :exec
DECLARE $owner_id AS Uint64;
DECLARE $rows AS List<Struct<record_id: Utf8, group_id: Utf8, payload: Bytes, attributes: Json>>;
DECLARE $created_at AS Timestamp;
INSERT INTO records (owner_hash, owner_id, record_id, group_id, payload, attributes, created_at, updated_at)
SELECT
    Digest::CityHash(CAST($owner_id AS String)) AS owner_hash,
    $owner_id AS owner_id,
    r.record_id,
    r.group_id,
    r.payload,
    r.attributes,
    $created_at AS created_at,
    $created_at AS updated_at
FROM AS_TABLE($rows) AS r;

-- name: UpdateRecords :exec
DECLARE $owner_id AS Uint64;
DECLARE $rows AS List<Struct<record_id: Utf8, group_id: Utf8, payload: Bytes, attributes: Json>>;
DECLARE $updated_at AS Timestamp;
UPDATE records ON
SELECT
    Digest::CityHash(CAST($owner_id AS String)) AS owner_hash,
    r.*,
    $updated_at AS updated_at
FROM AS_TABLE($rows) AS r;

-- name: ListRecords :many
DECLARE $owner_id AS Uint64;
SELECT owner_hash, owner_id, record_id, group_id, payload, attributes, created_at, updated_at, note
FROM records
WHERE owner_hash = Digest::CityHash(CAST($owner_id AS String))
ORDER BY record_id;

-- name: FilterRecords :many
DECLARE $owner_id AS Uint64;
DECLARE $record_ids AS List<Utf8>;
DECLARE $group_id AS Utf8;
SELECT owner_hash, owner_id, record_id, group_id, payload, attributes, created_at, updated_at
FROM records
WHERE owner_hash = Digest::CityHash(CAST($owner_id AS String))
    AND record_id IN $record_ids
    AND group_id = $group_id
ORDER BY record_id;

-- name: DeleteRecords :exec
DECLARE $owner_id AS Uint64;
DECLARE $record_ids AS List<Utf8>;
DECLARE $group_id AS Utf8;
DELETE FROM records ON
SELECT owner_hash, record_id
FROM records
WHERE owner_hash = Digest::CityHash(CAST($owner_id AS String))
    AND record_id IN $record_ids
    AND group_id = $group_id;

-- name: GetRecord :one
DECLARE $key AS Struct<owner_hash: Uint64, record_id: Utf8>;
SELECT owner_hash, owner_id, record_id, group_id, payload, attributes, created_at, updated_at
FROM records
WHERE owner_hash = $key.owner_hash AND record_id = $key.record_id;

-- name: InsertNamedRecords :exec
DECLARE $owner_id AS Uint64;
DECLARE $rows AS List<Struct<payload: Bytes, attributes: Json, group_id: Utf8, record_id: Utf8, note: Utf8,>>;
DECLARE $created_at AS Timestamp;
INSERT INTO records
SELECT
    $created_at AS updated_at,
    r.*,
    Digest::CityHash(CAST($owner_id AS String)) AS owner_hash,
    $owner_id AS owner_id,
    $created_at AS created_at
FROM AS_TABLE($rows) AS r;

-- name: UpsertNamedRecords :exec
DECLARE $owner_hash AS Uint64;
DECLARE $owner_id AS Uint64;
DECLARE $created_at AS Timestamp;
DECLARE $rows AS List<Struct<body: Bytes, key: Utf8, group_id: Utf8, attributes: Json,>>;
UPSERT INTO records
SELECT r.body AS payload, r.key AS record_id, $owner_hash AS owner_hash,
    $owner_id AS owner_id, r.group_id, r.attributes,
    $created_at AS created_at, $created_at AS updated_at
FROM AS_TABLE($rows) AS r;

-- name: UpsertWildcardRecords :exec
DECLARE $rows AS List<Struct<payload: Bytes, record_id: Utf8, owner_hash: Uint64, group_id: Utf8, owner_id: Uint64, attributes: Json, created_at: Timestamp, updated_at: Timestamp,>>;
UPSERT INTO records
SELECT * FROM AS_TABLE($rows);

-- name: ClearNamedRecordNote :one
DECLARE $owner_hash AS Uint64;
DECLARE $record_id AS Utf8;
UPSERT INTO records
SELECT record_id, owner_hash, owner_id, group_id, payload, attributes, created_at, updated_at, NULL AS note
FROM records
WHERE owner_hash = $owner_hash AND record_id = $record_id
RETURNING record_id, note;

-- name: UpsertPositionalRecord :one
DECLARE $owner_hash AS Uint64;
DECLARE $record_id AS Utf8;
DECLARE $payload AS Bytes;
UPSERT INTO records (payload, record_id, owner_hash, owner_id, group_id, attributes, created_at, updated_at)
SELECT $payload AS record_id, record_id AS owner_hash, owner_hash AS payload,
    owner_id, group_id, attributes, created_at, updated_at
FROM records
WHERE owner_hash = $owner_hash AND record_id = $record_id
RETURNING record_id, payload, note;
