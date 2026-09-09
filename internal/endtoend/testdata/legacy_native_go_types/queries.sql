-- name: BindNativeTypes :one
DECLARE $ids AS List<Uint64>;
DECLARE $optional_ids AS List<Optional<Uint64>>;
DECLARE $amount AS Decimal(22, 9);
DECLARE $id AS Uuid;
SELECT $ids AS ids, $optional_ids AS optional_ids, $amount AS amount, $id AS id;
