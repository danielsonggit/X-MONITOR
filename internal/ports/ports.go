package ports

import (
	"context"
	"time"

	"github.com/sudoHG/x-monitor/internal/domain"
)

type SearchRequest struct {
	Accounts     []string
	ContentTypes []domain.ContentType
	WindowStart  time.Time
	WindowEnd    time.Time
	Depth        string
}

type SearchResult struct {
	RunID      string
	Posts      []domain.Post
	ResultPath string
	Partial    bool
}

type Searcher interface {
	Search(context.Context, SearchRequest) (SearchResult, error)
	Check(context.Context) error
}

type Store interface {
	Migrate(context.Context) error
	SyncAccounts(context.Context, []string) error
	StartRun(context.Context, domain.Run) error
	CompleteRun(context.Context, domain.Run) error
	LastRun(context.Context) (*domain.Run, error)
	LastSuccessfulRun(context.Context) (*domain.Run, error)
	InsertPostsAndDeliveries(context.Context, string, []domain.Post, string, time.Time, *time.Time) ([]domain.Post, error)
	ClaimNextDelivery(context.Context, string, time.Time, time.Time) (*domain.Delivery, error)
	MarkDeliverySent(context.Context, int64, int64, time.Time) error
	MarkDeliveryRetry(context.Context, int64, time.Time, string, time.Time) error
	MarkDeliveryDead(context.Context, int64, string, time.Time) error
	RecoverExpiredDeliveries(context.Context, time.Time) error
	ServiceStatus(context.Context) (domain.ServiceStatus, error)
	Close() error
}

type Notifier interface {
	Send(context.Context, domain.Notification) (domain.Receipt, error)
	Check(context.Context) error
}

type DeliveryDispatcher interface {
	Dispatch(context.Context) (int64, error)
}

type RunLocker interface {
	TryLock(context.Context) (unlock func() error, acquired bool, err error)
}
