package domain

import "time"

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusPartial   RunStatus = "partial"
	RunStatusFailed    RunStatus = "failed"
	RunStatusSkipped   RunStatus = "skipped"
)

type TriggerType string

const (
	TriggerScheduled TriggerType = "scheduled"
	TriggerStartup   TriggerType = "startup"
	TriggerManual    TriggerType = "manual"
)

type Run struct {
	ID         string      `json:"id"`
	Trigger    TriggerType `json:"trigger"`
	Scheduled  *time.Time  `json:"scheduled_at,omitempty"`
	StartedAt  time.Time   `json:"started_at"`
	Completed  *time.Time  `json:"completed_at,omitempty"`
	WindowFrom time.Time   `json:"window_start"`
	WindowTo   time.Time   `json:"window_end"`
	Status     RunStatus   `json:"status"`
	GrokRunID  string      `json:"grok_run_id,omitempty"`
	PostsFound int64       `json:"posts_found"`
	PostsNew   int64       `json:"posts_new"`
	Sent       int64       `json:"notifications_sent"`
	ErrorCode  string      `json:"error_code,omitempty"`
	Error      string      `json:"error,omitempty"`
}

type ServiceStatus struct {
	LastRun             *Run       `json:"last_run"`
	LastSuccessfulRun   *Run       `json:"last_successful_run"`
	PendingDeliveries   int64      `json:"pending_deliveries"`
	RetryDeliveries     int64      `json:"retry_deliveries"`
	DeadDeliveries      int64      `json:"dead_deliveries"`
	LastSuccessfulAtUTC *time.Time `json:"last_successful_at_utc,omitempty"`
}
