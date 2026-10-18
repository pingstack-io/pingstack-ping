//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

var forwardedSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

func forward(p *os.Process, sig os.Signal) { _ = p.Signal(sig) }

// exitStatus follows the shell convention: 128 + the signal for a killed job.
func exitStatus(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return err.ExitCode()
}
