package filelock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/fx"
)

type Lock struct {
	file *flock.Flock
}

var Module = fx.Module(
	"filelock",
	fx.Provide(
		fx.Annotate(New, fx.As(new(ports.RunLocker))),
	),
)

func New(cfg config.Config) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Storage.LockFile), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	return &Lock{file: flock.New(cfg.Storage.LockFile)}, nil
}

func (l *Lock) TryLock(ctx context.Context) (func() error, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	locked, err := l.file.TryLock()
	if err != nil {
		return nil, false, fmt.Errorf("try monitor file lock: %w", err)
	}
	if !locked {
		return func() error { return nil }, false, nil
	}
	return func() error {
		if err := l.file.Unlock(); err != nil {
			return fmt.Errorf("unlock monitor file lock: %w", err)
		}
		return nil
	}, true, nil
}

var _ ports.RunLocker = (*Lock)(nil)
