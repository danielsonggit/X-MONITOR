package app

import (
	"github.com/sudoHG/x-monitor/internal/adapters/filelock"
	"github.com/sudoHG/x-monitor/internal/adapters/grok"
	"github.com/sudoHG/x-monitor/internal/adapters/sqlite"
	"github.com/sudoHG/x-monitor/internal/adapters/telegram"
	"github.com/sudoHG/x-monitor/internal/application/delivery"
	"github.com/sudoHG/x-monitor/internal/application/monitor"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/infrastructure/logging"
	"go.uber.org/fx"
)

var Core = fx.Options(
	config.Module,
	logging.Module,
	sqlite.Module,
	filelock.Module,
	grok.Module,
	telegram.Module,
	delivery.Module,
	monitor.Module,
)

var Storage = fx.Options(
	config.Module,
	logging.Module,
	sqlite.Module,
)
