package grok

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/zap"
)

func TestSearchRunsFakeCLIInIsolatedEnvironment(t *testing.T) {
	temp := t.TempDir()
	realHome := filepath.Join(temp, "real-home")
	require.NoError(t, os.MkdirAll(filepath.Join(realHome, ".grok"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(realHome, ".grok", "auth.json"),
		[]byte(`{"access_token":"test-only"}`),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(realHome, "secret-marker"),
		[]byte("must not be copied"),
		0o600,
	))
	t.Setenv("HOME", realHome)
	t.Setenv("XMONITOR_TELEGRAM_TOKEN", "must-not-reach-grok")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:7891")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7891")
	t.Setenv("ALL_PROXY", "socks5h://127.0.0.1:7891")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost,::1")

	fakeCLI := filepath.Join(temp, "fake-grok")
	script := `#!/bin/sh
set -eu
test ! -e "$HOME/secret-marker"
test -z "${XMONITOR_TELEGRAM_TOKEN:-}"
test "${HTTP_PROXY:-}" = "http://127.0.0.1:7891"
test "${HTTPS_PROXY:-}" = "http://127.0.0.1:7891"
test "${ALL_PROXY:-}" = "socks5h://127.0.0.1:7891"
test "${NO_PROXY:-}" = "127.0.0.1,localhost,::1"
case " $* " in
  *" --tools x_search "*) ;;
  *) echo "x_search was not the exclusive tool" >&2; exit 70 ;;
esac
printf '%s\n' '{"text":"{\"schema_version\":1,\"posts\":[{\"account\":\"@CryptoDinduz\",\"type\":\"original\",\"published_at\":\"2026-07-31T01:30:00Z\",\"status_url\":\"https://twitter.com/CryptoDinduz/status/123\",\"text\":\"hello\",\"summary_zh\":\"测试摘要。\",\"uncertainties\":[]}]}"}'
`
	require.NoError(t, os.WriteFile(fakeCLI, []byte(script), 0o700))
	runsDirectory := filepath.Join(temp, "runs")
	client, err := New(config.Config{
		Grok: config.Grok{
			Binary:        fakeCLI,
			Model:         "grok-test",
			Depth:         "quick",
			Timeout:       20 * time.Second,
			MaxTurns:      2,
			RetentionDays: 1,
			MaxOutputSize: 1 << 20,
		},
		Storage: config.Storage{RunsDirectory: runsDirectory},
	}, zap.NewNop())
	require.NoError(t, err)

	result, err := client.Search(context.Background(), ports.SearchRequest{
		Accounts:    []string{"@CryptoDinduz"},
		WindowStart: time.Date(2026, 7, 31, 1, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC),
		Depth:       "quick",
	})

	require.NoError(t, err)
	require.False(t, result.Partial)
	require.Len(t, result.Posts, 1)
	require.Equal(t, "https://x.com/CryptoDinduz/status/123", result.Posts[0].StatusURL)
	require.FileExists(t, result.ResultPath)
	resultInfo, err := os.Stat(result.ResultPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), resultInfo.Mode().Perm())
	require.FileExists(t, filepath.Join(filepath.Dir(result.ResultPath), "manifest.json"))
}
