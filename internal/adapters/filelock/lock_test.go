package filelock

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
)

func TestOnlyOneLockInstanceAcquiresFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "x-monitor.lock")
	first, err := New(config.Config{Storage: config.Storage{LockFile: path}})
	require.NoError(t, err)
	second, err := New(config.Config{Storage: config.Storage{LockFile: path}})
	require.NoError(t, err)

	unlockFirst, acquired, err := first.TryLock(context.Background())
	require.NoError(t, err)
	require.True(t, acquired)

	_, acquired, err = second.TryLock(context.Background())
	require.NoError(t, err)
	require.False(t, acquired)

	require.NoError(t, unlockFirst())
	unlockSecond, acquired, err := second.TryLock(context.Background())
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, unlockSecond())
}
