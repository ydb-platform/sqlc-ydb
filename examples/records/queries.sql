-- name: InsertRecords :exec
DECLARE $owner_id AS Uint64;
DECLARE $rows AS List<Struct<record_id: Utf8, group_id: Utf8, payload: Bytes, attributes: Json>>;
DECLARE $created_at AS Timestamp;
INSERT INTO records (owner_hash, owner_id, record_id, group_id, payload, attributes, created_at, updated_at)
SELECT
    Digest::CityHash(CAST($owner_id AS String)),
    $owner_id,
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
SELECT *
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

-- name: UpsertRecords :exec
DECLARE $owner_id AS Uint64;
DECLARE $rows AS List<Struct<record_id: Utf8, group_id: Utf8, payload: Bytes, attributes: Json>>;
DECLARE $created_at AS Timestamp;
UPSERT INTO records (owner_hash, owner_id, record_id, group_id, payload, attributes, created_at, updated_at)
SELECT
    Digest::CityHash(CAST($owner_id AS String)),
    $owner_id,
    r.record_id,
    r.group_id,
    r.payload,
    r.attributes,
    $created_at,
    $created_at
FROM AS_TABLE($rows) AS r;

-- name: FindRecordsByTags :many
DECLARE $tags AS List<String>;
SELECT record_id
FROM records
WHERE NOT SetIsDisjoint(ToSet(Yson::ConvertToStringList(attributes)), $tags)
ORDER BY record_id;

-- name: ReverseGroupLabel :one
DECLARE $label AS Utf8?;
SELECT Unicode::Reverse($label) AS reversed_label;
