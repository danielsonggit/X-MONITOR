//go:build windows

package grok

import (
	"os"
	"os/exec"
)

func configureProcess(_ *exec.Cmd) {}

func killProcessTree(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Kill()
}
