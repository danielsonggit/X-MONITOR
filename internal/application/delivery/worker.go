package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/google/uuid"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Worker struct {
	cfg      config.Config
	store    ports.Store
	notifier ports.Notifier
	logger   *zap.Logger
}

var Module = fx.Module(
	"delivery",
	fx.Provide(
		fx.Annotate(New, fx.As(new(ports.DeliveryDispatcher))),
	),
)

func New(cfg config.Config, store ports.Store, notifier ports.Notifier, logger *zap.Logger) *Worker {
	return &Worker{
		cfg:      cfg,
		store:    store,
		notifier: notifier,
		logger:   logger.Named("delivery"),
	}
}

func (w *Worker) Dispatch(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	if err := w.store.RecoverExpiredDeliveries(ctx, now); err != nil {
		return 0, fmt.Errorf("recover expired deliveries: %w", err)
	}
	owner := uuid.NewString()
	var sent int64
	for {
		readyAt := time.Now().UTC()
		delivery, err := w.store.ClaimNextDelivery(
			ctx,
			owner,
			readyAt,
			readyAt.Add(w.cfg.Telegram.Lease),
		)
		if err != nil {
			return sent, err
		}
		if delivery == nil {
			return sent, nil
		}
		receipt, sendErr := w.sendWithRetry(ctx, *delivery)
		now = time.Now().UTC()
		if sendErr == nil {
			if err := w.store.MarkDeliverySent(ctx, delivery.ID, receipt.MessageID, receipt.SentAt); err != nil {
				return sent, fmt.Errorf("mark delivery sent: %w", err)
			}
			sent++
			w.logger.Info("Telegram delivery sent",
				zap.Int64("delivery_id", delivery.ID),
				zap.String("status_url", delivery.Post.StatusURL),
				zap.Int64("telegram_message_id", receipt.MessageID),
			)
			continue
		}
		var notificationError *domain.NotificationError
		_ = errors.As(sendErr, &notificationError)
		permanent := errors.Is(sendErr, backoff.ErrPermanent) ||
			(notificationError != nil && notificationError.IsPermanent)
		if permanent || delivery.Attempts >= int64(w.cfg.Telegram.MaxAttempts) {
			if err := w.store.MarkDeliveryDead(ctx, delivery.ID, truncateError(sendErr), now); err != nil {
				return sent, fmt.Errorf("mark delivery dead: %w", err)
			}
			w.logger.Error("Telegram delivery permanently failed",
				zap.Int64("delivery_id", delivery.ID),
				zap.String("status_url", delivery.Post.StatusURL),
				zap.Error(sendErr),
			)
			continue
		}
		delay := w.retryDelay(delivery.Attempts)
		if notificationError != nil && notificationError.RetryAfterDelay > delay {
			delay = notificationError.RetryAfterDelay
		}
		if err := w.store.MarkDeliveryRetry(
			ctx,
			delivery.ID,
			now.Add(delay),
			truncateError(sendErr),
			now,
		); err != nil {
			return sent, fmt.Errorf("mark delivery retry: %w", err)
		}
		w.logger.Warn("Telegram delivery scheduled for retry",
			zap.Int64("delivery_id", delivery.ID),
			zap.Duration("retry_after", delay),
			zap.Error(sendErr),
		)
	}
}

func (w *Worker) sendWithRetry(ctx context.Context, delivery domain.Delivery) (domain.Receipt, error) {
	policy := backoff.NewExponentialBackOff()
	policy.InitialInterval = 2 * time.Second
	policy.MaxInterval = 30 * time.Second
	receipt, err := backoff.Retry(
		ctx,
		func() (domain.Receipt, error) {
			result, err := w.notifier.Send(ctx, domain.Notification{
				Post:        delivery.Post,
				Destination: delivery.Destination,
			})
			if err == nil {
				return result, nil
			}
			var notificationError *domain.NotificationError
			if errors.As(err, &notificationError) {
				if notificationError.IsPermanent {
					return result, backoff.Permanent(err)
				}
				if notificationError.RetryAfterDelay > 0 {
					return result, backoff.RetryAfter(notificationError.RetryAfterDelay, err)
				}
			}
			return result, err
		},
		backoff.WithBackOff(policy),
		backoff.WithMaxTries(3),
		backoff.WithMaxElapsedTime(2*time.Minute),
		backoff.WithNotify(func(err error, delay time.Duration) {
			w.logger.Warn("retrying Telegram request",
				zap.Int64("delivery_id", delivery.ID),
				zap.Duration("delay", delay),
				zap.Error(err),
			)
		}),
	)
	return receipt, err
}

func (w *Worker) retryDelay(attempts int64) time.Duration {
	policy := backoff.NewExponentialBackOff()
	policy.InitialInterval = 5 * time.Minute
	policy.RandomizationFactor = 0
	policy.Multiplier = 2
	policy.MaxInterval = 6 * time.Hour
	policy.Reset()
	delay := policy.NextBackOff()
	for index := int64(1); index < attempts; index++ {
		delay = policy.NextBackOff()
	}
	return delay
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 4000 {
		return value[:4000]
	}
	return value
}

var _ ports.DeliveryDispatcher = (*Worker)(nil)
