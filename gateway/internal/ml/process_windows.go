//go:build windows

package ml

import (
	"os"
	"os/exec"
)

func configureManagedProcess(*exec.Cmd) {}

func terminateManagedProcess(cmd *exec.Cmd) error {
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

func killManagedProcess(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
