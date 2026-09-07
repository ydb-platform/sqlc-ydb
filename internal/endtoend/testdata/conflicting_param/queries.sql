-- name: BrokenParam :one
DECLARE $value AS Uint64;
DECLARE $value AS Utf8;
SELECT id FROM authors WHERE id = $value;
