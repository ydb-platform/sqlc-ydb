-- name: PutCustomer :exec
UPSERT INTO customers (id, name, note) VALUES ($id, $name, $note);

-- name: GetCustomer :one
SELECT id, name, note FROM customers WHERE id = $id;
