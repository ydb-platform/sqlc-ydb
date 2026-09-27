-- name: GetItem :one
SELECT id, label FROM items WHERE id = $id;

-- name: FindItem :many
SELECT id, label FROM items WHERE id = $id AND label = $label;

-- name: AllItems :many
SELECT id, label FROM items;
