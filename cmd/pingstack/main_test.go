package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as the job: run with PINGSTACK_TEST_JOB set, it
// prints and exits as told instead of running tests.
func TestMain(m *testing.M) {
	if os.Getenv("PINGSTACK_TEST_JOB") != "" {
		fakeJob()
	}
	testHooks = func(o *options) *options {
		o.anyHost = true
		o.backoff = func(int) time.Duration { return time.Millisecond }
		return o
	}
	os.Exit(m.Run())
}

func fakeJob() {
	lines, _ := strconv.Atoi(os.Getenv("PINGSTACK_TEST_LINES"))
	for i := 1; i <= lines; i++ {
		fmt.Printf("line %d\n", i)
	}
	fmt.Fprintln(os.Stderr, "to stderr")
	if d, err := time.ParseDuration(os.Getenv("PINGSTACK_TEST_SLEEP")); err == nil {
		time.Sleep(d)
	}
	code, _ := strconv.Atoi(os.Getenv("PINGSTACK_TEST_CODE"))
	os.Exit(code)
}

type request struct{ path, query, body string }

type server struct {
	*httptest.Server
	mu       sync.Mutex
	requests []request
	status   int
}

func newServer(t *testing.T) *server {
	s := &server{status: 200}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, request{r.URL.Path, r.URL.RawQuery, string(body)})
		status := s.status
		s.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) got() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request{}, s.requests...)
}

// runJobCLI runs "pingstack run <flags> -- <this test binary>" as a job exiting with code.
func runJobCLI(t *testing.T, url string, code, lines int, flags ...string) (int, string, string) {
	t.Helper()
	t.Setenv("PINGSTACK_TEST_JOB", "1")
	t.Setenv("PINGSTACK_TEST_CODE", strconv.Itoa(code))
	t.Setenv("PINGSTACK_TEST_LINES", strconv.Itoa(lines))
	args := append([]string{"run"}, flags...)
	args = append(args, "--", os.Args[0])
	var stdout, stderr bytes.Buffer
	env := map[string]string{"PINGSTACK_PING_URL": url}
	exit := cli(args, &stdout, &stderr, func(k string) string { return env[k] })
	return exit, stdout.String(), stderr.String()
}

func TestRunReportsStartThenSuccess(t *testing.T) {
	s := newServer(t)
	exit, stdout, stderr := runJobCLI(t, s.URL+"/ping/tok", 0, 2)
	if exit != 0 || stdout != "line 1\nline 2\n" || !strings.Contains(stderr, "to stderr") {
		t.Fatalf("exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	got := s.got()
	if len(got) != 2 || got[0].path != "/ping/tok/start" || got[1].path != "/ping/tok" || got[1].query != "exit_code=0" || got[1].body != "" {
		t.Fatalf("requests %+v", got)
	}
}

func TestRunReportsAFailureWithTheEndOfTheOutput(t *testing.T) {
	s := newServer(t)
	exit, _, stderr := runJobCLI(t, s.URL+"/ping/tok", 3, 150)
	if exit != 3 || !strings.Contains(stderr, "reported the failure (exit code 3)") {
		t.Fatalf("exit %d, stderr %q", exit, stderr)
	}
	fail := s.got()[1]
	if fail.path != "/ping/tok/fail" || fail.query != "exit_code=3" {
		t.Fatalf("got %+v", fail)
	}
	if strings.Contains(fail.body, "line 50\n") || !strings.Contains(fail.body, "line 150\n") || strings.Count(fail.body, "\n") != 100 {
		t.Fatalf("body kept the wrong lines: %q", fail.body)
	}
}

func TestRunTailZeroSendsNoOutput(t *testing.T) {
	s := newServer(t)
	runJobCLI(t, s.URL+"/ping/tok", 1, 5, "--tail", "0")
	if body := s.got()[1].body; body != "" {
		t.Fatalf("body %q", body)
	}
}

func TestRunValueFromOutput(t *testing.T) {
	s := newServer(t)
	runJobCLI(t, s.URL+"/ping/tok", 0, 7, "--value-from-output")
	if q := s.got()[1].query; q != "exit_code=0&value=7" {
		t.Fatalf("query %q", q)
	}
}

func TestRunNoStart(t *testing.T) {
	s := newServer(t)
	runJobCLI(t, s.URL+"/ping/tok", 0, 0, "--no-start")
	if got := s.got(); len(got) != 1 || got[0].path != "/ping/tok" {
		t.Fatalf("requests %+v", got)
	}
}

func TestRunCommandNotFound(t *testing.T) {
	s := newServer(t)
	var stderr bytes.Buffer
	env := map[string]string{"PINGSTACK_PING_URL": s.URL + "/ping/tok"}
	exit := cli([]string{"run", "--", "pingstack-no-such-command"}, io.Discard, &stderr, func(k string) string { return env[k] })
	fail := s.got()[1]
	if exit != 127 || fail.path != "/ping/tok/fail" || !strings.Contains(fail.body, "couldn't run the command") {
		t.Fatalf("exit %d, %+v", exit, fail)
	}
}

func TestRunStillRunsTheJobWithABadURL(t *testing.T) {
	exit, stdout, stderr := runJobCLI(t, "not a url", 4, 1)
	if exit != 4 || stdout != "line 1\n" || !strings.Contains(stderr, "running the job without reporting") {
		t.Fatalf("exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
}

func TestRunKeepsTheJobsExitCodeWhenPingstackIsDown(t *testing.T) {
	s := newServer(t)
	url := s.URL + "/ping/SECRET_TOKEN"
	s.Close()
	exit, _, stderr := runJobCLI(t, url, 0, 0, "--retries", "1")
	if exit != 0 || !strings.Contains(stderr, "couldn't report the success") || strings.Contains(stderr, "SECRET_TOKEN") {
		t.Fatalf("exit %d, stderr %q", exit, stderr)
	}
}

func TestRunQuiet(t *testing.T) {
	s := newServer(t)
	s.status = 404
	_, _, stderr := runJobCLI(t, s.URL+"/ping/tok", 2, 0, "--quiet")
	if stderr != "to stderr\n" {
		t.Fatalf("stderr %q", stderr)
	}
}

func TestPing(t *testing.T) {
	s := newServer(t)
	env := map[string]string{"PINGSTACK_PING_URL": s.URL + "/ping/tok"}
	getenv := func(k string) string { return env[k] }
	for _, args := range [][]string{{"ping"}, {"ping", "--fail", "--exit-code", "9", "--message", "disk full"}, {"ping", "--start"}, {"ping", "--value", "12.5"}} {
		if exit := cli(args, io.Discard, io.Discard, getenv); exit != 0 {
			t.Fatalf("%v: exit %d", args, exit)
		}
	}
	want := []request{{"/ping/tok", "", ""}, {"/ping/tok/fail", "exit_code=9", "disk full"}, {"/ping/tok/start", "", ""}, {"/ping/tok", "value=12.5", ""}}
	if got := s.got(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %+v", got)
	}
}

func TestPingFailsWhenNotDelivered(t *testing.T) {
	s := newServer(t)
	s.status = 404
	var stderr bytes.Buffer
	exit := cli([]string{"ping", "--url", s.URL + "/ping/tok"}, io.Discard, &stderr, func(string) string { return "" })
	if exit != 1 || !strings.Contains(stderr.String(), "404") {
		t.Fatalf("exit %d, stderr %q", exit, stderr.String())
	}
}

func TestArgumentErrors(t *testing.T) {
	none := func(string) string { return "" }
	for _, args := range [][]string{{}, {"nope"}, {"run"}, {"ping"}, {"ping", "--url", "https://pingstack.io/ping/x", "--fail", "--start"}, {"ping", "--url", "https://pingstack.io/ping/x", "--value", "lots"}} {
		if exit := cli(args, io.Discard, io.Discard, none); exit != 2 {
			t.Errorf("%v: exit %d, want 2", args, exit)
		}
	}
}

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	if cli([]string{"version"}, &stdout, io.Discard, nil) != 0 || stdout.String() != "pingstack dev\n" {
		t.Fatalf("got %q", stdout.String())
	}
}
