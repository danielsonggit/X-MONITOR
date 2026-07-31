package monitor

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/sudoHG/x-monitor/internal/adapters/sqlite"
	"github.com/sudoHG/x-monitor/internal/application/delivery"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type testLifecycle struct {
	hooks []fx.Hook
}

func (l *testLifecycle) Append(hook fx.Hook) {
	l.hooks = append(l.hooks, hook)
}

type fakeSearcher struct {
	mu       sync.Mutex
	requests []ports.SearchRequest
}

func (s *fakeSearcher) Search(
	_ context.Context,
	request ports.SearchRequest,
) (ports.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, request)
	published := request.WindowEnd.Add(-30 * time.Minute)
	return ports.SearchResult{
		RunID: "grok-test-run",
		Posts: []domain.Post{{
			StatusURL:    "https://x.com/CryptoDinduz/status/100",
			StatusID:     "100",
			Account:      "@CryptoDinduz",
			ContentType:  domain.ContentTypeOriginal,
			PublishedAt:  &published,
			OriginalText: "test",
			SummaryZH:    "集成测试摘要。",
			RawJSON:      `{}`,
		}},
	}, nil
}

func (s *fakeSearcher) Check(context.Context) error {
	return nil
}

type fakeNotifier struct {
	mu            sync.Mutex
	notifications []domain.Notification
}

func (n *fakeNotifier) Send(
	_ context.Context,
	notification domain.Notification,
) (domain.Receipt, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notifications = append(n.notifications, notification)
	return domain.Receipt{
		MessageID: int64(len(n.notifications)),
		SentAt:    time.Now().UTC(),
	}, nil
}

func (n *fakeNotifier) Check(context.Context) error {
	return nil
}

type fakeLocker struct{}

func (fakeLocker) TryLock(context.Context) (func() error, bool, error) {
	return func() error { return nil }, true, nil
}

func TestRunOncePersistsDeduplicatesAndDispatches(t *testing.T) {
	temp := t.TempDir()
	cfg := config.Config{
		Monitor: config.Monitor{
			Accounts:             []string{"@CryptoDinduz"},
			ContentTypes:         []string{"original", "reply", "repost"},
			NormalWindow:         2 * time.Hour,
			FirstRunReportWindow: time.Hour,
			RecoveryMaxWindow:    24 * time.Hour,
			RecoveryOverlap:      15 * time.Minute,
		},
		Grok: config.Grok{Depth: "quick"},
		Telegram: config.Telegram{
			ChatID:      "-100123",
			MaxAttempts: 8,
			Lease:       15 * time.Minute,
		},
		Storage: config.Storage{
			Database: filepath.Join(temp, "x-monitor.db"),
		},
	}
	store, err := sqlite.New(&testLifecycle{}, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	searcher := &fakeSearcher{}
	notifier := &fakeNotifier{}
	dispatcher := delivery.New(cfg, store, notifier, zap.NewNop())
	service := New(cfg, store, searcher, fakeLocker{}, dispatcher, zap.NewNop())

	require.NoError(t, service.RunOnce(context.Background(), domain.TriggerManual))
	require.NoError(t, service.RunOnce(context.Background(), domain.TriggerManual))

	notifier.mu.Lock()
	require.Len(t, notifier.notifications, 1, "same status URL must only be sent once")
	require.Equal(t, "https://x.com/CryptoDinduz/status/100",
		notifier.notifications[0].Post.StatusURL)
	notifier.mu.Unlock()

	status, err := service.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status.LastRun)
	require.Equal(t, domain.RunStatusSucceeded, status.LastRun.Status)
	require.Equal(t, int64(0), status.LastRun.PostsNew)
	require.Equal(t, int64(0), status.PendingDeliveries)

	searcher.mu.Lock()
	require.Len(t, searcher.requests, 2)
	require.Equal(t, 2*time.Hour,
		searcher.requests[0].WindowEnd.Sub(searcher.requests[0].WindowStart))
	require.Equal(t, []domain.ContentType{
		domain.ContentTypeOriginal,
		domain.ContentTypeReply,
		domain.ContentTypeRepost,
	}, searcher.requests[0].ContentTypes)
	searcher.mu.Unlock()
}

func TestCalculateWindowRecoversAfterDowntimeWithinCap(t *testing.T) {
	t.Parallel()

	service := &Service{cfg: config.Config{Monitor: config.Monitor{
		NormalWindow:      2 * time.Hour,
		RecoveryMaxWindow: 24 * time.Hour,
		RecoveryOverlap:   15 * time.Minute,
	}}}
	now := time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC)
	completed := now.Add(-30 * time.Hour)

	start, firstRun := service.calculateWindow(now, &domain.Run{Completed: &completed})

	require.False(t, firstRun)
	require.Equal(t, now.Add(-24*time.Hour), start)
}
