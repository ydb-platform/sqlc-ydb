-- name: UpsertAccount :exec
DECLARE $account_id AS Uint64;
DECLARE $display_name AS Utf8;
UPSERT INTO accounts (account_id, display_name) VALUES ($account_id, $display_name);

-- name: GetAccount :one
DECLARE $account_id AS Uint64;
SELECT account_id, display_name FROM accounts WHERE account_id = $account_id;

-- name: ListAccountModels :many
SELECT sqlc.embed(a) FROM accounts AS a;
