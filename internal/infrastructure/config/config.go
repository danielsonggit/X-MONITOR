package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"go.uber.org/fx"
)

const (
	DefaultPath     = "configs/config.toml"
	configEnvPrefix = "XMONITOR_CFG_"
)

type Config struct {
	Scheduler Scheduler
	Monitor   Monitor
	Grok      Grok
	Telegram  Telegram
	Storage   Storage
	Logging   Logging
}

type Scheduler struct {
	Cron         string
	Timezone     string
	RunOnStartup bool
	JobTimeout   time.Duration
}

type Monitor struct {
	Accounts             []string
	ContentTypes         []string
	NormalWindow         time.Duration
	FirstRunReportWindow time.Duration
	RecoveryMaxWindow    time.Duration
	RecoveryOverlap      time.Duration
}

type Grok struct {
	Binary        string
	Model         string
	Depth         string
	Timeout       time.Duration
	MaxTurns      int
	RetentionDays int
	MaxOutputSize int64
}

type Telegram struct {
	ChatID      string
	TokenEnv    string
	Token       string
	MaxAttempts int
	Lease       time.Duration
}

type Storage struct {
	Database      string
	RunsDirectory string
	LockFile      string
}

type Logging struct {
	Level  string
	Format string
}

type rawConfig struct {
	Scheduler struct {
		Cron         string `koanf:"cron"`
		Timezone     string `koanf:"timezone"`
		RunOnStartup bool   `koanf:"run_on_startup"`
		JobTimeout   string `koanf:"job_timeout"`
	} `koanf:"scheduler"`
	Monitor struct {
		Accounts             []string `koanf:"accounts"`
		ContentTypes         []string `koanf:"content_types"`
		NormalWindow         string   `koanf:"normal_window"`
		FirstRunReportWindow string   `koanf:"first_run_report_window"`
		RecoveryMaxWindow    string   `koanf:"recovery_max_window"`
		RecoveryOverlap      string   `koanf:"recovery_overlap"`
	} `koanf:"monitor"`
	Grok struct {
		Binary        string `koanf:"binary"`
		Model         string `koanf:"model"`
		Depth         string `koanf:"depth"`
		Timeout       string `koanf:"timeout"`
		MaxTurns      int    `koanf:"max_turns"`
		RetentionDays int    `koanf:"retention_days"`
		MaxOutputSize int64  `koanf:"max_output_size"`
	} `koanf:"grok"`
	Telegram struct {
		ChatID      string `koanf:"chat_id"`
		TokenEnv    string `koanf:"token_env"`
		MaxAttempts int    `koanf:"max_attempts"`
		Lease       string `koanf:"lease"`
	} `koanf:"telegram"`
	Storage struct {
		Database      string `koanf:"database"`
		RunsDirectory string `koanf:"runs_directory"`
		LockFile      string `koanf:"lock_file"`
	} `koanf:"storage"`
	Logging struct {
		Level  string `koanf:"level"`
		Format string `koanf:"format"`
	} `koanf:"logging"`
}

type Path string

var Module = fx.Module("config", fx.Provide(func(path Path) (Config, error) {
	return Load(string(path))
}))

func Load(path string) (Config, error) {
	raw := defaults()
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return Config{}, fmt.Errorf("load config file %s: %w", path, err)
	}
	if err := k.Load(env.Provider(configEnvPrefix, ".", func(key string) string {
		key = strings.TrimPrefix(key, configEnvPrefix)
		return strings.ToLower(strings.ReplaceAll(key, "__", "."))
	}), nil); err != nil {
		return Config{}, fmt.Errorf("load config environment: %w", err)
	}
	if err := k.Unmarshal("", &raw); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	cfg, err := parse(raw)
	if err != nil {
		return Config{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaults() rawConfig {
	var raw rawConfig
	raw.Scheduler.Cron = "0 * * * *"
	raw.Scheduler.Timezone = "Asia/Shanghai"
	raw.Scheduler.RunOnStartup = true
	raw.Scheduler.JobTimeout = "50m"
	raw.Monitor.ContentTypes = []string{"original", "reply", "repost"}
	raw.Monitor.NormalWindow = "2h"
	raw.Monitor.FirstRunReportWindow = "1h"
	raw.Monitor.RecoveryMaxWindow = "24h"
	raw.Monitor.RecoveryOverlap = "15m"
	raw.Grok.Model = "grok-4.5"
	raw.Grok.Depth = "quick"
	raw.Grok.Timeout = "10m"
	raw.Grok.MaxTurns = 40
	raw.Grok.RetentionDays = 7
	raw.Grok.MaxOutputSize = 32 << 20
	raw.Telegram.TokenEnv = "XMONITOR_TELEGRAM_TOKEN"
	raw.Telegram.MaxAttempts = 8
	raw.Telegram.Lease = "15m"
	raw.Logging.Level = "info"
	raw.Logging.Format = "json"
	return raw
}

func parse(raw rawConfig) (Config, error) {
	parseDuration := func(name, value string) (time.Duration, error) {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("parse %s duration %q: %w", name, value, err)
		}
		return duration, nil
	}
	jobTimeout, err := parseDuration("scheduler.job_timeout", raw.Scheduler.JobTimeout)
	if err != nil {
		return Config{}, err
	}
	normalWindow, err := parseDuration("monitor.normal_window", raw.Monitor.NormalWindow)
	if err != nil {
		return Config{}, err
	}
	firstWindow, err := parseDuration("monitor.first_run_report_window", raw.Monitor.FirstRunReportWindow)
	if err != nil {
		return Config{}, err
	}
	recoveryWindow, err := parseDuration("monitor.recovery_max_window", raw.Monitor.RecoveryMaxWindow)
	if err != nil {
		return Config{}, err
	}
	recoveryOverlap, err := parseDuration("monitor.recovery_overlap", raw.Monitor.RecoveryOverlap)
	if err != nil {
		return Config{}, err
	}
	grokTimeout, err := parseDuration("grok.timeout", raw.Grok.Timeout)
	if err != nil {
		return Config{}, err
	}
	lease, err := parseDuration("telegram.lease", raw.Telegram.Lease)
	if err != nil {
		return Config{}, err
	}
	token := os.Getenv(raw.Telegram.TokenEnv)
	return Config{
		Scheduler: Scheduler{
			Cron:         raw.Scheduler.Cron,
			Timezone:     raw.Scheduler.Timezone,
			RunOnStartup: raw.Scheduler.RunOnStartup,
			JobTimeout:   jobTimeout,
		},
		Monitor: Monitor{
			Accounts:             raw.Monitor.Accounts,
			ContentTypes:         raw.Monitor.ContentTypes,
			NormalWindow:         normalWindow,
			FirstRunReportWindow: firstWindow,
			RecoveryMaxWindow:    recoveryWindow,
			RecoveryOverlap:      recoveryOverlap,
		},
		Grok: Grok{
			Binary:        raw.Grok.Binary,
			Model:         raw.Grok.Model,
			Depth:         raw.Grok.Depth,
			Timeout:       grokTimeout,
			MaxTurns:      raw.Grok.MaxTurns,
			RetentionDays: raw.Grok.RetentionDays,
			MaxOutputSize: raw.Grok.MaxOutputSize,
		},
		Telegram: Telegram{
			ChatID:      raw.Telegram.ChatID,
			TokenEnv:    raw.Telegram.TokenEnv,
			Token:       token,
			MaxAttempts: raw.Telegram.MaxAttempts,
			Lease:       lease,
		},
		Storage: Storage{
			Database:      raw.Storage.Database,
			RunsDirectory: raw.Storage.RunsDirectory,
			LockFile:      raw.Storage.LockFile,
		},
		Logging: Logging{
			Level:  raw.Logging.Level,
			Format: raw.Logging.Format,
		},
	}, nil
}

func validateConfig(cfg Config) error {
	validate := validator.New(validator.WithRequiredStructEnabled())
	type validationConfig struct {
		Cron          string   `validate:"required"`
		Timezone      string   `validate:"required,timezone"`
		Accounts      []string `validate:"required,min=1,dive,required"`
		ContentTypes  []string `validate:"required,min=1,dive,oneof=original reply repost"`
		GrokModel     string   `validate:"required"`
		GrokDepth     string   `validate:"required,oneof=quick deep"`
		TelegramChat  string   `validate:"required"`
		Database      string   `validate:"required"`
		RunsDirectory string   `validate:"required"`
		LockFile      string   `validate:"required"`
		LogLevel      string   `validate:"required,oneof=debug info warn error"`
		LogFormat     string   `validate:"required,oneof=json console"`
	}
	required := validationConfig{
		Cron:          cfg.Scheduler.Cron,
		Timezone:      cfg.Scheduler.Timezone,
		Accounts:      cfg.Monitor.Accounts,
		ContentTypes:  cfg.Monitor.ContentTypes,
		GrokModel:     cfg.Grok.Model,
		GrokDepth:     cfg.Grok.Depth,
		TelegramChat:  cfg.Telegram.ChatID,
		Database:      cfg.Storage.Database,
		RunsDirectory: cfg.Storage.RunsDirectory,
		LockFile:      cfg.Storage.LockFile,
		LogLevel:      cfg.Logging.Level,
		LogFormat:     cfg.Logging.Format,
	}
	if err := validate.Struct(required); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	seen := make(map[string]struct{}, len(cfg.Monitor.Accounts))
	for _, account := range cfg.Monitor.Accounts {
		if !regexp.MustCompile(`^@[A-Za-z0-9_]{1,15}$`).MatchString(account) {
			return fmt.Errorf("validate config: invalid account %q", account)
		}
		normalized := strings.ToLower(account)
		if _, ok := seen[normalized]; ok {
			return fmt.Errorf("validate config: duplicate account %q", account)
		}
		seen[normalized] = struct{}{}
	}
	if cfg.Monitor.NormalWindow <= 0 ||
		cfg.Monitor.FirstRunReportWindow <= 0 ||
		cfg.Monitor.RecoveryMaxWindow < cfg.Monitor.NormalWindow ||
		cfg.Monitor.FirstRunReportWindow > cfg.Monitor.NormalWindow {
		return errors.New("validate config: invalid monitor window relationship")
	}
	if cfg.Scheduler.JobTimeout <= 0 || cfg.Grok.Timeout <= 0 || cfg.Grok.Timeout > cfg.Scheduler.JobTimeout {
		return errors.New("validate config: Grok timeout must be positive and no longer than job timeout")
	}
	if cfg.Telegram.MaxAttempts < 1 || cfg.Telegram.Lease <= 0 {
		return errors.New("validate config: invalid Telegram retry settings")
	}
	if cfg.Grok.MaxTurns < 1 || cfg.Grok.RetentionDays < 0 || cfg.Grok.MaxOutputSize < 1024 {
		return errors.New("validate config: invalid Grok limits")
	}
	if !slices.Contains([]string{"quick", "deep"}, cfg.Grok.Depth) {
		return errors.New("validate config: invalid Grok depth")
	}
	return nil
}
