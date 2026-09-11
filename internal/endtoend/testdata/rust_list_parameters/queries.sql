-- name: FindIds :many
SELECT id FROM records WHERE id IN $ids ORDER BY id;

-- name: ExcludeIds :many
SELECT id FROM records WHERE id NOT IN $ids ORDER BY id;

-- name: FindSingle :many
SELECT id FROM records WHERE id IN ($id);
