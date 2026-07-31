package domain

import (
	"fmt"
	"time"
)

type NotificationError struct {
	Cause           error
	IsPermanent     bool
	RetryAfterDelay time.Duration
}

func (e *NotificationError) Error() string {
	if e == nil || e.Cause == nil {
		return "notification error"
	}
	return e.Cause.Error()
}

func (e *NotificationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type SearchError struct {
	Code    string
	Message string
	Path    string
	Cause   error
}

func (e *SearchError) Error() string {
	if e == nil {
		return "search error"
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *SearchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
