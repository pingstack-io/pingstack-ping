//go:build windows

package main

import (
	"os"
	"os/exec"
)

// Ctrl-C reaches every process on the console, the job included, so
// pingstack only has to stay alive long enough to report.
var forwardedSignals = []os.Signal{os.Interrupt}

func forward(*os.Process, os.Signal) {}

func exitStatus(err *exec.ExitError) int { return err.ExitCode() }
