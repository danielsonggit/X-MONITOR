-- +goose Up
PRAGMA foreign_keys = ON;

CREATE TABLE accounts (
    handle          TEXT PRIMARY KEY,
    enabled         INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at_ms   INTEGER NOT NULL
);

CREATE TABLE runs (
    id                    TEXT PRIMARY KEY,
    trigger_type          TEXT NOT NULL CHECK (trigger_type IN ('scheduled', 'startup', 'manual')),
    scheduled_at_ms       INTEGER,
    started_at_ms         INTEGER NOT NULL,
    completed_at_ms       INTEGER,
    window_start_ms       INTEGER NOT NULL,
    window_end_ms         INTEGER NOT NULL,
    status                TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'partial', 'failed', 'skipped')),
    grok_run_id           TEXT,
    posts_found           INTEGER NOT NULL DEFAULT 0,
    posts_new             INTEGER NOT NULL DEFAULT 0,
    notifications_sent    INTEGER NOT NULL DEFAULT 0,
    error_code            TEXT,
    error_message         TEXT
);

CREATE INDEX idx_runs_started_at ON runs(started_at_ms DESC);
CREATE INDEX idx_runs_success ON runs(status, completed_at_ms DESC);

CREATE TABLE posts (
    status_url          TEXT PRIMARY KEY,
    status_id           TEXT NOT NULL UNIQUE,
    account_handle      TEXT NOT NULL,
    content_type        TEXT NOT NULL CHECK (content_type IN ('original', 'reply', 'repost', 'unknown')),
    published_at_ms     INTEGER,
    original_text       TEXT,
    summary_zh          TEXT NOT NULL,
    uncertainty_json    TEXT NOT NULL DEFAULT '[]',
    first_seen_run_id   TEXT NOT NULL,
    first_seen_at_ms    INTEGER NOT NULL,
    raw_json            TEXT NOT NULL,
    FOREIGN KEY (account_handle) REFERENCES accounts(handle),
    FOREIGN KEY (first_seen_run_id) REFERENCES runs(id)
);

CREATE INDEX idx_posts_published_at ON posts(published_at_ms);
CREATE INDEX idx_posts_account ON posts(account_handle, published_at_ms);

CREATE TABLE deliveries (
    id                        INTEGER PRIMARY KEY AUTOINCREMENT,
    status_url                TEXT NOT NULL,
    destination               TEXT NOT NULL,
    state                     TEXT NOT NULL CHECK (state IN ('pending', 'processing', 'sent', 'retry', 'dead')),
    attempts                  INTEGER NOT NULL DEFAULT 0,
    next_attempt_at_ms        INTEGER,
    processing_owner          TEXT,
    processing_expires_at_ms  INTEGER,
    telegram_message_id       INTEGER,
    sent_at_ms                INTEGER,
    last_error                TEXT,
    created_at_ms             INTEGER NOT NULL,
    updated_at_ms             INTEGER NOT NULL,
    FOREIGN KEY (status_url) REFERENCES posts(status_url),
    UNIQUE (status_url, destination)
);

CREATE INDEX idx_deliveries_ready
ON deliveries(state, next_attempt_at_ms, created_at_ms);

CREATE TABLE monitor_state (
    key             TEXT PRIMARY KEY,
    value           TEXT NOT NULL,
    updated_at_ms   INTEGER NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS monitor_state;
DROP TABLE IF EXISTS deliveries;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS accounts;
