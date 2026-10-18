// Command pingstack reports scheduled jobs to a Pingstack heartbeat check.
//
//	pingstack run -- ./backup.sh     run a job and report how it went
//	pingstack ping [--fail|--start]  send one report
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pingstack-io/pingstack-ping/internal/report"
)

// Set at build time by GoReleaser.
var version = "dev"

const usage = `pingstack reports scheduled jobs to a Pingstack heartbeat check.

Usage:
  pingstack run [flags] -- <command> [args...]
  pingstack ping [flags]
  pingstack version

The ping URL comes from --url or the PINGSTACK_PING_URL environment variable.
Run "pingstack run -h" or "pingstack ping -h" for each command's flags.
More: https://pingstack.io
`

func main() {
	os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// options shared by run and ping.
type options struct {
	url     string
	value   string
	retries int
	timeout time.Duration
	quiet   bool
	anyHost bool // tests only
	backoff func(int) time.Duration
	stderr  io.Writer
}

func cli(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout, stderr, getenv)
	case "ping":
		return pingCommand(args[1:], stderr, getenv)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "pingstack %s\n", version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "pingstack: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func commonFlags(fs *flag.FlagSet, o *options, getenv func(string) string) {
	fs.StringVar(&o.url, "url", getenv("PINGSTACK_PING_URL"), "the check's ping URL (default $PINGSTACK_PING_URL)")
	fs.StringVar(&o.value, "value", "", "a number to check against the check's thresholds")
	fs.IntVar(&o.retries, "retries", 5, "retries after a network error, 429 or 5xx")
	fs.DurationVar(&o.timeout, "timeout", 10*time.Second, "time to wait for each request")
	fs.BoolVar(&o.quiet, "quiet", false, "print nothing of pingstack's own, except errors in its arguments")
}

func (o *options) client() *report.Client {
	return &report.Client{
		URL: o.url, Retries: o.retries, Timeout: o.timeout, Backoff: o.backoff,
		AllowAnyHost: o.anyHost, UserAgent: "pingstack/" + version,
	}
}

func (o *options) validate() error {
	if strings.TrimSpace(o.url) == "" {
		return errors.New("no ping URL: set PINGSTACK_PING_URL or pass --url")
	}
	if _, err := report.ValidateURL(o.url, o.anyHost); err != nil {
		return err
	}
	if o.value != "" {
		if _, err := strconv.ParseFloat(o.value, 64); err != nil {
			return fmt.Errorf("--value must be a number, got %q", o.value)
		}
	}
	if o.retries < 0 {
		return errors.New("--retries can't be negative")
	}
	return nil
}

func (o *options) warn(format string, args ...any) {
	if !o.quiet {
		fmt.Fprintf(o.stderr, "pingstack: "+format+"\n", args...)
	}
}

// pingCommand sends one report and exits 1 if it couldn't be delivered.
func pingCommand(args []string, stderr io.Writer, getenv func(string) string) int {
	o := testHooks(&options{stderr: stderr})
	fs := flag.NewFlagSet("pingstack ping", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, o, getenv)
	fail := fs.Bool("fail", false, "report that the job failed")
	start := fs.Bool("start", false, "report that the job is starting, so its run time is recorded")
	exitCode := fs.Int("exit-code", -1, "the job's exit code, recorded with the report")
	message := fs.String("message", "", "text stored with the report")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: pingstack ping [flags]\n\nSends one report to the check.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "pingstack ping: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *fail && *start {
		fmt.Fprintln(stderr, "pingstack ping: use --fail or --start, not both")
		return 2
	}
	if err := o.validate(); err != nil {
		fmt.Fprintf(stderr, "pingstack ping: %v\n", err)
		return 2
	}
	r := report.Report{Kind: report.Success, Value: o.value, Body: *message}
	switch {
	case *fail:
		r.Kind = report.Failure
	case *start:
		r.Kind = report.Start
	}
	if *exitCode >= 0 {
		r.ExitCode = exitCode
	}
	if err := o.client().Send(context.Background(), r); err != nil {
		fmt.Fprintf(stderr, "pingstack: %v\n", err)
		return 1
	}
	return 0
}

var lastNumber = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// lastNumberIn finds the last number in a job's output, for --value-from-output.
func lastNumberIn(output string) string {
	matches := lastNumber.FindAllString(output, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// testHooks lets tests point a command at a local server without a flag
// anyone could use in production.
var testHooks = func(o *options) *options { return o }
