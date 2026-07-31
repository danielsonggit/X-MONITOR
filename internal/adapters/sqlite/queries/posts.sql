-- name: InsertPost :execresult
INSERT OR IGNORE INTO posts (
    status_url,
    status_id,
    account_handle,
    content_type,
    published_at_ms,
    original_text,
    summary_zh,
    uncertainty_json,
    first_seen_run_id,
    first_seen_at_ms,
    raw_json
) VALUES (
    sqlc.arg(status_url),
    sqlc.arg(status_id),
    sqlc.arg(account_handle),
    sqlc.arg(content_type),
    sqlc.narg(published_at_ms),
    sqlc.narg(original_text),
    sqlc.arg(summary_zh),
    sqlc.arg(uncertainty_json),
    sqlc.arg(first_seen_run_id),
    sqlc.arg(first_seen_at_ms),
    sqlc.arg(raw_json)
);

-- name: GetPostByStatusURL :one
SELECT *
FROM posts
WHERE status_url = sqlc.arg(status_url);
