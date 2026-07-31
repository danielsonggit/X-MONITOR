package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadMergesDefaultsFileAndEnvironment(t *testing.T) {
	temp := t.TempDir()
	configPath := filepath.Join(temp, "config.toml")
	content := `
[monitor]
accounts = ["@CryptoDinduz", "@2442lll"]

[telegram]
chat_id = "-1001234567890"

[storage]
database = "` + filepath.Join(temp, "data", "x-monitor.db") + `"
runs_directory = "` + filepath.Join(temp, "runs") + `"
lock_file = "` + filepath.Join(temp, "x-monitor.lock") + `"
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
	t.Setenv("XMONITOR_TELEGRAM_TOKEN", "test-token")
	t.Setenv("XMONITOR_CFG_SCHEDULER__CRON", "15 * * * *")
	t.Setenv("XMONITOR_CFG_LOGGING__LEVEL", "debug")

	cfg, err := Load(configPath)

	require.NoError(t, err)
	require.Equal(t, "15 * * * *", cfg.Scheduler.Cron)
	require.Equal(t, "Asia/Shanghai", cfg.Scheduler.Timezone)
	require.Equal(t, 50*time.Minute, cfg.Scheduler.JobTimeout)
	require.Equal(t, []string{"original", "reply", "repost"}, cfg.Monitor.ContentTypes)
	require.Equal(t, 2*time.Hour, cfg.Monitor.NormalWindow)
	require.Equal(t, time.Hour, cfg.Monitor.FirstRunReportWindow)
	require.Equal(t, "grok-4.5", cfg.Grok.Model)
	require.Equal(t, "quick", cfg.Grok.Depth)
	require.Equal(t, "test-token", cfg.Telegram.Token)
	require.Equal(t, "debug", cfg.Logging.Level)
}

func TestLoadRejectsDuplicateAccountsCaseInsensitively(t *testing.T) {
	temp := t.TempDir()
	configPath := filepath.Join(temp, "config.toml")
	content := `
[monitor]
accounts = ["@CryptoDinduz", "@cryptodinduz"]

[telegram]
chat_id = "123"

[storage]
database = "` + filepath.Join(temp, "x-monitor.db") + `"
runs_directory = "` + filepath.Join(temp, "runs") + `"
lock_file = "` + filepath.Join(temp, "x-monitor.lock") + `"
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
	t.Setenv("XMONITOR_TELEGRAM_TOKEN", "test-token")

	_, err := Load(configPath)

	require.ErrorContains(t, err, "duplicate account")
}
