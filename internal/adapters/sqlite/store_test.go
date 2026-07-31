package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"go.uber.org/fx"
)

type lifecycleStub struct {
	hooks []fx.Hook
}

func (l *lifecycleStub) Append(hook fx.Hook) {
	l.hooks = append(l.hooks, hook)
}

func TestStoreMigratesDeduplicatesAndDelivers(t *testing.T) {
	temp := t.TempDir()
	lifecycle := &lifecycleStub{}
	cfg := config.Config{Storage: config.Storage{
		Database: filepath.Join(temp, "data", "x-monitor.db"),
	}}
	store, err := New(lifecycle, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	ctx := context.Background()
	require.NoError(t, store.SyncAccounts(ctx, []string{"@CryptoDinduz"}))

	started := time.Date(2026, 7, 31, 1, 0, 0, 0, time.UTC)
	run := domain.Run{
		ID:         "run-1",
		Trigger:    domain.TriggerManual,
		StartedAt:  started,
		WindowFrom: started.Add(-2 * time.Hour),
		WindowTo:   started,
		Status:     domain.RunStatusRunning,
	}
	require.NoError(t, store.StartRun(ctx, run))
	published := started.Add(-30 * time.Minute)
	post := domain.Post{
		StatusURL:    "https://x.com/CryptoDinduz/status/100",
		StatusID:     "100",
		Account:      "@CryptoDinduz",
		ContentType:  domain.ContentTypeOriginal,
		PublishedAt:  &published,
		OriginalText: "hello",
		SummaryZH:    "测试摘要。",
		RawJSON:      `{"status_url":"https://x.com/CryptoDinduz/status/100"}`,
	}

	inserted, err := store.InsertPostsAndDeliveries(
		ctx, run.ID, []domain.Post{post}, "-100123", started, nil,
	)
	require.NoError(t, err)
	require.Equal(t, []domain.Post{post}, inserted)

	inserted, err = store.InsertPostsAndDeliveries(
		ctx, run.ID, []domain.Post{post}, "-100123", started, nil,
	)
	require.NoError(t, err)
	require.Empty(t, inserted)

	delivery, err := store.ClaimNextDelivery(ctx, "worker-1", started, started.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.Equal(t, int64(1), delivery.Attempts)
	require.Equal(t, post.StatusURL, delivery.Post.StatusURL)

	sentAt := started.Add(time.Second)
	require.NoError(t, store.MarkDeliverySent(ctx, delivery.ID, 42, sentAt))
	run.Status = domain.RunStatusSucceeded
	run.PostsFound = 1
	run.PostsNew = 1
	run.Sent = 1
	run.Completed = &sentAt
	require.NoError(t, store.CompleteRun(ctx, run))

	status, err := store.ServiceStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, status.LastSuccessfulRun)
	require.Equal(t, run.ID, status.LastSuccessfulRun.ID)
	require.Zero(t, status.PendingDeliveries)
	require.Zero(t, status.RetryDeliveries)
	require.Zero(t, status.DeadDeliveries)
}

func TestStoreSuppressesOldAndUncertainFirstRunDeliveries(t *testing.T) {
	temp := t.TempDir()
	store, err := New(&lifecycleStub{}, config.Config{Storage: config.Storage{
		Database: filepath.Join(temp, "x-monitor.db"),
	}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	ctx := context.Background()
	require.NoError(t, store.SyncAccounts(ctx, []string{"@2442lll"}))
	now := time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC)
	run := domain.Run{
		ID:         "run-first",
		Trigger:    domain.TriggerStartup,
		StartedAt:  now,
		WindowFrom: now.Add(-2 * time.Hour),
		WindowTo:   now,
		Status:     domain.RunStatusRunning,
	}
	require.NoError(t, store.StartRun(ctx, run))
	old := now.Add(-90 * time.Minute)
	posts := []domain.Post{
		{
			StatusURL:   "https://x.com/2442lll/status/200",
			StatusID:    "200",
			Account:     "@2442lll",
			ContentType: domain.ContentTypeRepost,
			PublishedAt: &old,
			SummaryZH:   "较旧内容。",
			RawJSON:     `{}`,
		},
		{
			StatusURL:   "https://x.com/2442lll/status/201",
			StatusID:    "201",
			Account:     "@2442lll",
			ContentType: domain.ContentTypeUnknown,
			SummaryZH:   "时间不确定。",
			RawJSON:     `{}`,
		},
	}
	cutoff := now.Add(-time.Hour)
	inserted, err := store.InsertPostsAndDeliveries(
		ctx, run.ID, posts, "123", now, &cutoff,
	)
	require.NoError(t, err)
	require.Len(t, inserted, 2)

	delivery, err := store.ClaimNextDelivery(ctx, "worker", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Nil(t, delivery)
}
