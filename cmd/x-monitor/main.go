package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/sudoHG/x-monitor/internal/commands"
	"github.com/sudoHG/x-monitor/internal/infrastructure/buildinfo"
)

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	err := commands.Execute(buildinfo.Info{
		Version:   version,
		Commit:    commit,
		BuiltAt:   builtAt,
		GoVersion: runtime.Version(),
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
