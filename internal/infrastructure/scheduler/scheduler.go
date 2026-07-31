package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/sudoHG/x-monitor/internal/application/monitor"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Scheduler struct {
	cfg       config.Config
	scheduler gocron.Scheduler
	service   *monitor.Service
	logger    *zap.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

var Module = fx.Module(
	"scheduler",
	fx.Provide(New),
	fx.Invoke(func(*Scheduler) {}),
)

func New(
	lifecycle fx.Lifecycle,
	cfg config.Config,
	service *monitor.Service,
	logger *zap.Logger,
) (*Scheduler, error) {
	location, err := time.LoadLocation(cfg.Scheduler.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load scheduler timezone: %w", err)
	}
	engine, err := gocron.NewScheduler(
		gocron.WithLocation(location),
		gocron.WithStopTimeout(time.Minute),
	)
	if err != nil {
		return nil, fmt.Errorf("create gocron scheduler: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	instance := &Scheduler{
		cfg:       cfg,
		scheduler: engine,
		service:   service,
		logger:    logger.Named("scheduler"),
		ctx:       ctx,
		cancel:    cancel,
	}
	if _, err := engine.NewJob(
		gocron.CronJob(cfg.Scheduler.Cron, false),
		gocron.NewTask(instance.runScheduled),
		gocron.WithName("x-account-monitor"),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
		gocron.WithContext(ctx),
	); err != nil {
		cancel()
		_ = engine.Shutdown()
		return nil, fmt.Errorf("register monitor job: %w", err)
	}
	lifecycle.Append(fx.Hook{
		OnStart: instance.start,
		OnStop:  instance.stop,
	})
	return instance, nil
}

func (s *Scheduler) start(_ context.Context) error {
	s.scheduler.Start()
	s.logger.Info("scheduler started",
		zap.String("cron", s.cfg.Scheduler.Cron),
		zap.String("timezone", s.cfg.Scheduler.Timezone),
	)
	if s.cfg.Scheduler.RunOnStartup {
		go func() {
			runCtx, cancel := context.WithTimeout(s.ctx, s.cfg.Scheduler.JobTimeout)
			defer cancel()
			if err := s.service.RunOnce(runCtx, domain.TriggerStartup); err != nil {
				s.logger.Error("startup monitor run failed", zap.Error(err))
			}
		}()
	}
	return nil
}

func (s *Scheduler) stop(_ context.Context) error {
	s.cancel()
	if err := s.scheduler.Shutdown(); err != nil {
		return fmt.Errorf("shutdown scheduler: %w", err)
	}
	s.logger.Info("scheduler stopped")
	return nil
}

func (s *Scheduler) runScheduled() {
	runCtx, cancel := context.WithTimeout(s.ctx, s.cfg.Scheduler.JobTimeout)
	defer cancel()
	if err := s.service.RunOnce(runCtx, domain.TriggerScheduled); err != nil {
		s.logger.Error("scheduled monitor run failed", zap.Error(err))
	}
}
