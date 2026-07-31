package monitor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Service struct {
	cfg        config.Config
	store      ports.Store
	searcher   ports.Searcher
	locker     ports.RunLocker
	dispatcher ports.DeliveryDispatcher
	logger     *zap.Logger
}

var Module = fx.Module("monitor", fx.Provide(New))

func New(
	cfg config.Config,
	store ports.Store,
	searcher ports.Searcher,
	locker ports.RunLocker,
	dispatcher ports.DeliveryDispatcher,
	logger *zap.Logger,
) *Service {
	return &Service{
		cfg:        cfg,
		store:      store,
		searcher:   searcher,
		locker:     locker,
		dispatcher: dispatcher,
		logger:     logger.Named("monitor"),
	}
}

func (s *Service) RunOnce(ctx context.Context, trigger domain.TriggerType) error {
	unlock, acquired, err := s.locker.TryLock(ctx)
	if err != nil {
		return err
	}
	if !acquired {
		s.logger.Warn("monitor run skipped because another run is active",
			zap.String("trigger", string(trigger)),
		)
		return nil
	}
	defer func() {
		if err := unlock(); err != nil {
			s.logger.Error("failed to release monitor lock", zap.Error(err))
		}
	}()
	if err := s.store.SyncAccounts(ctx, s.cfg.Monitor.Accounts); err != nil {
		return err
	}
	now := time.Now().UTC()
	lastSuccess, err := s.store.LastSuccessfulRun(ctx)
	if err != nil {
		return err
	}
	windowStart, firstRun := s.calculateWindow(now, lastSuccess)
	run := domain.Run{
		ID:         uuid.NewString(),
		Trigger:    trigger,
		StartedAt:  now,
		WindowFrom: windowStart,
		WindowTo:   now,
		Status:     domain.RunStatusRunning,
	}
	if trigger == domain.TriggerScheduled {
		scheduled := now
		run.Scheduled = &scheduled
	}
	if err := s.store.StartRun(ctx, run); err != nil {
		return fmt.Errorf("start monitor run: %w", err)
	}
	started := time.Now()
	result, err := s.searcher.Search(ctx, ports.SearchRequest{
		Accounts:     s.cfg.Monitor.Accounts,
		ContentTypes: configuredContentTypes(s.cfg.Monitor.ContentTypes),
		WindowStart:  windowStart,
		WindowEnd:    now,
		Depth:        s.cfg.Grok.Depth,
	})
	if err != nil {
		run.Status = domain.RunStatusFailed
		run.ErrorCode, run.Error = classifySearchError(err)
		completed := time.Now().UTC()
		run.Completed = &completed
		if completeErr := s.store.CompleteRun(ctx, run); completeErr != nil {
			return errors.Join(err, completeErr)
		}
		return err
	}
	run.GrokRunID = result.RunID
	run.PostsFound = int64(len(result.Posts))
	sortPosts(result.Posts)
	var deliveryCutoff *time.Time
	if firstRun {
		cutoff := now.Add(-s.cfg.Monitor.FirstRunReportWindow)
		deliveryCutoff = &cutoff
	}
	inserted, err := s.store.InsertPostsAndDeliveries(
		ctx,
		run.ID,
		result.Posts,
		s.cfg.Telegram.ChatID,
		time.Now().UTC(),
		deliveryCutoff,
	)
	if err != nil {
		run.Status = domain.RunStatusFailed
		run.ErrorCode = "storage_failed"
		run.Error = truncate(err.Error())
		completed := time.Now().UTC()
		run.Completed = &completed
		if completeErr := s.store.CompleteRun(ctx, run); completeErr != nil {
			return errors.Join(err, completeErr)
		}
		return err
	}
	run.PostsNew = int64(len(inserted))
	sent, dispatchErr := s.dispatcher.Dispatch(ctx)
	run.Sent = sent
	run.Status = domain.RunStatusSucceeded
	if result.Partial {
		run.Status = domain.RunStatusPartial
		run.ErrorCode = "grok_partial_result"
		run.Error = "Grok returned usable structured output but the execution was partial"
	}
	if dispatchErr != nil {
		run.Status = domain.RunStatusPartial
		run.ErrorCode = "delivery_dispatch_failed"
		run.Error = truncate(dispatchErr.Error())
	}
	completed := time.Now().UTC()
	run.Completed = &completed
	if err := s.store.CompleteRun(ctx, run); err != nil {
		return fmt.Errorf("complete monitor run: %w", err)
	}
	s.logger.Info("monitor run completed",
		zap.String("run_id", run.ID),
		zap.String("status", string(run.Status)),
		zap.Time("window_start", windowStart),
		zap.Time("window_end", now),
		zap.Int64("posts_found", run.PostsFound),
		zap.Int64("posts_new", run.PostsNew),
		zap.Int64("notifications_sent", run.Sent),
		zap.Duration("duration", time.Since(started)),
	)
	return dispatchErr
}

func (s *Service) DispatchPending(ctx context.Context) (int64, error) {
	return s.dispatcher.Dispatch(ctx)
}

func (s *Service) Status(ctx context.Context) (domain.ServiceStatus, error) {
	return s.store.ServiceStatus(ctx)
}

func (s *Service) calculateWindow(now time.Time, lastSuccess *domain.Run) (time.Time, bool) {
	normal := now.Add(-s.cfg.Monitor.NormalWindow)
	if lastSuccess == nil || lastSuccess.Completed == nil {
		return normal, true
	}
	if now.Sub(*lastSuccess.Completed) <= s.cfg.Monitor.NormalWindow {
		return normal, false
	}
	start := lastSuccess.Completed.Add(-s.cfg.Monitor.RecoveryOverlap)
	earliest := now.Add(-s.cfg.Monitor.RecoveryMaxWindow)
	if start.Before(earliest) {
		start = earliest
	}
	return start, false
}

func classifySearchError(err error) (string, string) {
	var searchError *domain.SearchError
	if errors.As(err, &searchError) {
		return searchError.Code, truncate(searchError.Error())
	}
	return "search_failed", truncate(err.Error())
}

func sortPosts(posts []domain.Post) {
	sort.SliceStable(posts, func(left, right int) bool {
		if posts[left].PublishedAt == nil {
			return false
		}
		if posts[right].PublishedAt == nil {
			return true
		}
		return posts[left].PublishedAt.Before(*posts[right].PublishedAt)
	})
}

func configuredContentTypes(values []string) []domain.ContentType {
	result := make([]domain.ContentType, 0, len(values))
	for _, value := range values {
		result = append(result, domain.ParseContentType(value))
	}
	return result
}

func truncate(value string) string {
	if len(value) > 4000 {
		return value[:4000]
	}
	return value
}
