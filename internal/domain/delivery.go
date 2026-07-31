package domain

import "time"

type DeliveryState string

const (
	DeliveryPending    DeliveryState = "pending"
	DeliveryProcessing DeliveryState = "processing"
	DeliverySent       DeliveryState = "sent"
	DeliveryRetry      DeliveryState = "retry"
	DeliveryDead       DeliveryState = "dead"
)

type Delivery struct {
	ID                  int64
	Post                Post
	Destination         string
	State               DeliveryState
	Attempts            int64
	NextAttemptAt       *time.Time
	ProcessingOwner     string
	ProcessingExpiresAt *time.Time
	TelegramMessageID   int64
	SentAt              *time.Time
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Notification struct {
	Post        Post
	Destination string
}

type Receipt struct {
	MessageID int64
	SentAt    time.Time
}
