-- name: InsertDelivery :exec
INSERT OR IGNORE INTO deliveries (
    status_url,
    destination,
    state,
    attempts,
    created_at_ms,
    updated_at_ms
) VALUES (
    sqlc.arg(status_url),
    sqlc.arg(destination),
    'pending',
    0,
    sqlc.arg(created_at_ms),
    sqlc.arg(updated_at_ms)
);

-- name: ClaimNextDelivery :one
UPDATE deliveries
SET state = 'processing',
    attempts = attempts + 1,
    processing_owner = sqlc.arg(processing_owner),
    processing_expires_at_ms = sqlc.arg(processing_expires_at_ms),
    updated_at_ms = sqlc.arg(updated_at_ms)
WHERE deliveries.id = (
    SELECT candidate.id
    FROM deliveries AS candidate
    WHERE candidate.state = 'pending'
       OR (candidate.state = 'retry' AND (candidate.next_attempt_at_ms IS NULL OR candidate.next_attempt_at_ms <= sqlc.arg(ready_at_ms)))
       OR (candidate.state = 'processing' AND candidate.processing_expires_at_ms <= sqlc.arg(expired_at_ms))
    ORDER BY candidate.created_at_ms ASC, candidate.id ASC
    LIMIT 1
)
RETURNING *;

-- name: MarkDeliverySent :exec
UPDATE deliveries
SET state = 'sent',
    telegram_message_id = sqlc.arg(telegram_message_id),
    sent_at_ms = sqlc.arg(sent_at_ms),
    processing_owner = NULL,
    processing_expires_at_ms = NULL,
    next_attempt_at_ms = NULL,
    last_error = NULL,
    updated_at_ms = sqlc.arg(updated_at_ms)
WHERE id = sqlc.arg(id);

-- name: MarkDeliveryRetry :exec
UPDATE deliveries
SET state = 'retry',
    next_attempt_at_ms = sqlc.arg(next_attempt_at_ms),
    processing_owner = NULL,
    processing_expires_at_ms = NULL,
    last_error = sqlc.arg(last_error),
    updated_at_ms = sqlc.arg(updated_at_ms)
WHERE id = sqlc.arg(id);

-- name: MarkDeliveryDead :exec
UPDATE deliveries
SET state = 'dead',
    processing_owner = NULL,
    processing_expires_at_ms = NULL,
    last_error = sqlc.arg(last_error),
    updated_at_ms = sqlc.arg(updated_at_ms)
WHERE id = sqlc.arg(id);

-- name: RecoverExpiredDeliveries :exec
UPDATE deliveries
SET state = 'retry',
    next_attempt_at_ms = sqlc.arg(now_ms),
    processing_owner = NULL,
    processing_expires_at_ms = NULL,
    last_error = CASE
        WHEN last_error IS NULL OR last_error = '' THEN 'processing lease expired'
        ELSE last_error
    END,
    updated_at_ms = sqlc.arg(now_ms)
WHERE state = 'processing'
  AND processing_expires_at_ms <= sqlc.arg(now_ms);

-- name: GetDeliveryCounts :one
SELECT
    CAST(COALESCE(SUM(CASE WHEN state = 'pending' THEN 1 ELSE 0 END), 0) AS INTEGER) AS pending_count,
    CAST(COALESCE(SUM(CASE WHEN state = 'retry' THEN 1 ELSE 0 END), 0) AS INTEGER) AS retry_count,
    CAST(COALESCE(SUM(CASE WHEN state = 'dead' THEN 1 ELSE 0 END), 0) AS INTEGER) AS dead_count
FROM deliveries;
