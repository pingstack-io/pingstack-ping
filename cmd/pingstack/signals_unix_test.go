//go:build !windows

package main

import (
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunPassesSIGTERMToTheJobAndReportsTheFailure(t *testing.T) {
	s := newServer(t)
	t.Setenv("PINGSTACK_TEST_JOB", "1")
	t.Setenv("PINGSTACK_TEST_SLEEP", "10s")
	env := map[string]string{"PINGSTACK_PING_URL": s.URL + "/ping/tok"}

	done := make(chan int)
	go func() {
		done <- cli([]string{"run", "--", os.Args[0]}, io.Discard, io.Discard, func(k string) string { return env[k] })
	}()
	time.Sleep(500 * time.Millisecond)
	syscall.Kill(os.Getpid(), syscall.SIGTERM)

	select {
	case exit := <-done:
		got := s.got()
		if exit != 143 || len(got) != 2 || got[1].path != "/ping/tok/fail" || !strings.Contains(got[1].query, "exit_code=143") {
			t.Fatalf("exit %d, requests %+v", exit, got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job didn't stop on SIGTERM")
	}
}
