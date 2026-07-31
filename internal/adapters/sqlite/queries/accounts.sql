-- name: UpsertAccount :exec
INSERT INTO accounts (handle, enabled, created_at_ms)
VALUES (sqlc.arg(handle), 1, sqlc.arg(created_at_ms))
ON CONFLICT(handle) DO UPDATE SET enabled = 1;

-- name: DisableMissingAccounts :exec
UPDATE accounts
SET enabled = 0
WHERE handle NOT IN (sqlc.slice(handles));
