-- name: InsertRun :exec
INSERT INTO runs (
    id,
    trigger_type,
    scheduled_at_ms,
    started_at_ms,
    completed_at_ms,
    window_start_ms,
    window_end_ms,
    status,
    grok_run_id,
    posts_found,
    posts_new,
    notifications_sent,
    error_code,
    error_message
) VALUES (
    sqlc.arg(id),
    sqlc.arg(trigger_type),
    sqlc.narg(scheduled_at_ms),
    sqlc.arg(started_at_ms),
    NULL,
    sqlc.arg(window_start_ms),
    sqlc.arg(window_end_ms),
    sqlc.arg(status),
    NULL,
    0,
    0,
    0,
    NULL,
    NULL
);

-- name: CompleteRun :exec
UPDATE runs
SET completed_at_ms = sqlc.arg(completed_at_ms),
    status = sqlc.arg(status),
    grok_run_id = sqlc.narg(grok_run_id),
    posts_found = sqlc.arg(posts_found),
    posts_new = sqlc.arg(posts_new),
    notifications_sent = sqlc.arg(notifications_sent),
    error_code = sqlc.narg(error_code),
    error_message = sqlc.narg(error_message)
WHERE id = sqlc.arg(id);

-- name: GetLastRun :one
SELECT *
FROM runs
ORDER BY started_at_ms DESC, rowid DESC
LIMIT 1;

-- name: GetLastSuccessfulRun :one
SELECT *
FROM runs
WHERE status = 'succeeded'
ORDER BY completed_at_ms DESC, rowid DESC
LIMIT 1;
