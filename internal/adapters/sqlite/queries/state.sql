-- name: GetState :one
SELECT value
FROM monitor_state
WHERE key = sqlc.arg(key);

-- name: UpsertState :exec
INSERT INTO monitor_state (key, value, updated_at_ms)
VALUES (sqlc.arg(key), sqlc.arg(value), sqlc.arg(updated_at_ms))
ON CONFLICT(key) DO UPDATE SET
    value = excluded.value,
    updated_at_ms = excluded.updated_at_ms;
