package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"

	"github.com/pingstack-io/pingstack-ping/internal/report"
	"github.com/pingstack-io/pingstack-ping/internal/tail"
)

const maxTailBytes = 10 << 10

// runCommand runs a job, passing its output through, and reports the result.
// It always exits with the job's own exit code; a report that can't be
// delivered is a warning, never a reason to change it.
func runCommand(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	o := testHooks(&options{stderr: stderr})
	fs := flag.NewFlagSet("pingstack run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, o, getenv)
	tailLines := fs.Int("tail", 100, "lines of output to send with a failure (0 sends none; capped at 10 KB)")
	valueFromOutput := fs.Bool("value-from-output", false, "send the last number the job printed as --value")
	noStart := fs.Bool("no-start", false, "don't report the start, so no run time is recorded")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: pingstack run [flags] -- <command> [args...]\n\n"+
			"Runs the command, then reports success (exit code 0) or failure to the check.\n"+
			"Exits with the command's exit code.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	command := fs.Args()
	if len(command) == 0 {
		fmt.Fprintln(stderr, "pingstack run: no command given; usage: pingstack run [flags] -- <command> [args...]")
		return 2
	}
	if *valueFromOutput && o.value != "" {
		fmt.Fprintln(stderr, "pingstack run: use --value or --value-from-output, not both")
		return 2
	}

	// A bad ping URL shouldn't stop the job from running: warn, run it, skip reporting.
	reporting := true
	if err := o.validate(); err != nil {
		fmt.Fprintf(stderr, "pingstack: %v; running the job without reporting\n", err)
		reporting = false
	}
	client := o.client()
	ctx := context.Background()

	if reporting && !*noStart {
		startCtx, cancel := context.WithTimeout(ctx, o.timeout)
		// One quick try: a start that doesn't arrive only loses the run time.
		startClient := *client
		startClient.Retries = 0
		if err := startClient.Send(startCtx, report.Report{Kind: report.Start}); err != nil {
			o.warn("couldn't report the start (%v); the run time won't be recorded", err)
		}
		cancel()
	}

	keep := *tailLines
	if *valueFromOutput && keep < 20 {
		keep = 20
	}
	buffer := tail.New(keep, maxTailBytes)
	exitCode, runErr := runJob(command, stdout, stderr, buffer)

	if !reporting {
		return exitCode
	}
	r := report.Report{Kind: report.Success, ExitCode: &exitCode, Value: o.value}
	if *valueFromOutput {
		r.Value = lastNumberIn(buffer.String())
	}
	if exitCode != 0 {
		r.Kind = report.Failure
		if runErr != nil {
			r.Body = fmt.Sprintf("pingstack couldn't run the command: %v\n", runErr)
		}
		if *tailLines > 0 {
			r.Body += buffer.String()
		}
	}
	if err := client.Send(ctx, r); err != nil {
		o.warn("couldn't report the %s: %v", r.Kind, err)
	} else if !o.quiet && exitCode != 0 {
		fmt.Fprintf(stderr, "pingstack: reported the failure (exit code %d)\n", exitCode)
	}
	return exitCode
}

// runJob runs the command with its output copied to ours and to the tail
// buffer. Interrupts are passed to the job, and pingstack waits for it to
// exit before reporting.
func runJob(command []string, stdout, stderr io.Writer, buffer *tail.Buffer) (int, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(stdout, buffer)
	cmd.Stderr = io.MultiWriter(stderr, buffer)

	if err := cmd.Start(); err != nil {
		code := 126
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			code = 127
		}
		fmt.Fprintf(stderr, "pingstack: couldn't run %s: %v\n", command[0], err)
		return code, err
	}

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, forwardedSignals...)
	defer signal.Stop(signals)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-signals:
				forward(cmd.Process, sig)
			case <-done:
				return
			}
		}
	}()

	// Wait also waits for the output copies, so the tail is complete after it.
	err := cmd.Wait()
	close(done)
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitStatus(exitErr), nil
	}
	return 1, err
}
