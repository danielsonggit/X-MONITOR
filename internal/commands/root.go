package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"github.com/sudoHG/x-monitor/internal/app"
	"github.com/sudoHG/x-monitor/internal/application/monitor"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/buildinfo"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/infrastructure/scheduler"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/fx"
)

type rootOptions struct {
	configPath string
	build      buildinfo.Info
}

func Execute(build buildinfo.Info) error {
	options := &rootOptions{build: build}
	root := &cobra.Command{
		Use:           "x-monitor",
		Short:         "Monitor public X accounts through Grok and notify Telegram",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(
		&options.configPath,
		"config",
		"c",
		config.DefaultPath,
		"path to the TOML configuration file",
	)
	root.AddCommand(
		newServeCommand(options),
		newRunCommand(options),
		newStatusCommand(options),
		newDoctorCommand(options),
		newMigrateCommand(options),
		newVersionCommand(options),
	)
	return root.Execute()
}

func newServeCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the long-lived in-process scheduler",
		RunE: func(_ *cobra.Command, _ []string) error {
			application := fx.New(
				fx.Supply(config.Path(options.configPath)),
				app.Core,
				scheduler.Module,
			)
			application.Run()
			return application.Err()
		},
	}
}

func newRunCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run one monitor cycle immediately",
		RunE: func(command *cobra.Command, _ []string) error {
			var service *monitor.Service
			application := fx.New(
				fx.Supply(config.Path(options.configPath)),
				app.Core,
				fx.Populate(&service),
			)
			return runApplication(command.Context(), application, func(ctx context.Context) error {
				return service.RunOnce(ctx, domain.TriggerManual)
			})
		},
	}
}

func newStatusCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print persisted service status as JSON",
		RunE: func(command *cobra.Command, _ []string) error {
			var store ports.Store
			application := fx.New(
				fx.Supply(config.Path(options.configPath)),
				app.Storage,
				fx.Populate(&store),
			)
			return runApplication(command.Context(), application, func(ctx context.Context) error {
				status, err := store.ServiceStatus(ctx)
				if err != nil {
					return err
				}
				return writeJSON(status)
			})
		},
	}
}

func newDoctorCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration, database, Grok authentication, and Telegram",
		RunE: func(command *cobra.Command, _ []string) error {
			var searcher ports.Searcher
			var notifier ports.Notifier
			var store ports.Store
			application := fx.New(
				fx.Supply(config.Path(options.configPath)),
				app.Core,
				fx.Populate(&searcher, &notifier, &store),
			)
			return runApplication(command.Context(), application, func(ctx context.Context) error {
				checks := map[string]string{
					"config":   "ok",
					"database": "ok",
				}
				if err := searcher.Check(ctx); err != nil {
					checks["grok"] = err.Error()
				} else {
					checks["grok"] = "ok"
				}
				if err := notifier.Check(ctx); err != nil {
					checks["telegram"] = err.Error()
				} else {
					checks["telegram"] = "ok"
				}
				_ = store
				if err := writeJSON(checks); err != nil {
					return err
				}
				for _, value := range checks {
					if value != "ok" {
						return errors.New("one or more diagnostics failed")
					}
				}
				return nil
			})
		},
	}
}

func newMigrateCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply embedded SQLite migrations",
		RunE: func(command *cobra.Command, _ []string) error {
			var store ports.Store
			application := fx.New(
				fx.Supply(config.Path(options.configPath)),
				app.Storage,
				fx.Populate(&store),
			)
			return runApplication(command.Context(), application, func(ctx context.Context) error {
				if err := store.Migrate(ctx); err != nil {
					return err
				}
				return writeJSON(map[string]string{"migration": "ok"})
			})
		},
	}
}

func newVersionCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		RunE: func(_ *cobra.Command, _ []string) error {
			info := options.build
			if info.GoVersion == "" {
				info.GoVersion = runtime.Version()
			}
			return writeJSON(info)
		},
	}
}

func runApplication(
	parent context.Context,
	application *fx.App,
	run func(context.Context) error,
) error {
	if err := application.Err(); err != nil {
		return err
	}
	startContext, cancelStart := context.WithTimeout(parent, 30*time.Second)
	defer cancelStart()
	if err := application.Start(startContext); err != nil {
		return err
	}
	runErr := run(parent)
	stopContext, cancelStop := context.WithTimeout(context.Background(), time.Minute)
	defer cancelStop()
	stopErr := application.Stop(stopContext)
	return errors.Join(runErr, stopErr)
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write JSON output: %w", err)
	}
	return nil
}
