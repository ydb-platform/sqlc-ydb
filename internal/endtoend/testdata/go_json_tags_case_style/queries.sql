-- name: ListAccounts :many
SELECT account_id AS AccountID, account_id AS author_id, display_name FROM accounts;

-- name: RenameAccount :exec
DECLARE $account_id AS Uint64;
DECLARE $display_name AS Utf8;
UPDATE accounts SET display_name = $display_name WHERE account_id = $account_id;
