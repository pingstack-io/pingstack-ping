package report

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorded struct{ path, query, body, contentType string }

func stub(t *testing.T, statuses ...int) (*httptest.Server, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, recorded{r.URL.Path, r.URL.RawQuery, string(body), r.Header.Get("Content-Type")})
		status := statuses[len(statuses)-1]
		if len(got) <= len(statuses) {
			status = statuses[len(got)-1]
		}
		mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recorded { mu.Lock(); defer mu.Unlock(); return append([]recorded{}, got...) }
}

func client(url string, retries int) *Client {
	return &Client{URL: url, Retries: retries, Timeout: 2 * time.Second, AllowAnyHost: true,
		Backoff: func(int) time.Duration { return time.Millisecond }}
}

func TestValidateURL(t *testing.T) {
	for _, ok := range []string{"https://pingstack.io/ping/abc", "https://app.pingstack.io/ping/abc", "https://APP.PingStack.io:443/ping/abc"} {
		if _, err := ValidateURL(ok, false); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "http://pingstack.io/ping/abc", "https://example.com/ping/abc", "https://pingstack.io.example.com/x", "https://evilpingstack.io/x", "pingstack.io/ping/abc"} {
		if _, err := ValidateURL(bad, false); !errors.Is(err, ErrBadURL) {
			t.Errorf("%q: want ErrBadURL, got %v", bad, err)
		}
	}
}

func TestEndpoints(t *testing.T) {
	c := &Client{URL: "https://pingstack.io/ping/abc/"}
	code := 2
	cases := map[string]Report{
		"https://pingstack.io/ping/abc/":                       {Kind: Success},
		"https://pingstack.io/ping/abc/fail?exit_code=2":       {Kind: Failure, ExitCode: &code},
		"https://pingstack.io/ping/abc/start":                  {Kind: Start},
		"https://pingstack.io/ping/abc/?exit_code=2&value=4.5": {Kind: Success, ExitCode: &code, Value: "4.5"},
	}
	for want, r := range cases {
		if got, err := c.Endpoint(r); err != nil || got != want {
			t.Errorf("%v: got %q (%v), want %q", r.Kind, got, err, want)
		}
	}
}

func TestSendsTheBodyAsText(t *testing.T) {
	srv, got := stub(t, 200)
	if err := client(srv.URL+"/ping/tok", 0).Send(context.Background(), Report{Kind: Failure, Body: "boom\n"}); err != nil {
		t.Fatal(err)
	}
	r := got()[0]
	if r.path != "/ping/tok/fail" || r.body != "boom\n" || !strings.HasPrefix(r.contentType, "text/plain") {
		t.Fatalf("got %+v", r)
	}
}

func TestRetriesRateLimitsAndServerErrors(t *testing.T) {
	srv, got := stub(t, 503, 429, 200)
	if err := client(srv.URL+"/ping/tok", 3).Send(context.Background(), Report{}); err != nil {
		t.Fatal(err)
	}
	if n := len(got()); n != 3 {
		t.Fatalf("%d requests, want 3", n)
	}
}

func TestGivesUpAfterTheRetries(t *testing.T) {
	srv, got := stub(t, 502)
	err := client(srv.URL+"/ping/tok", 2).Send(context.Background(), Report{})
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "after 3 tries") {
		t.Fatalf("got %v", err)
	}
	if n := len(got()); n != 3 {
		t.Fatalf("%d requests, want 3", n)
	}
}

func TestDoesNotRetryOtherErrors(t *testing.T) {
	srv, got := stub(t, 404)
	err := client(srv.URL+"/ping/tok", 3).Send(context.Background(), Report{})
	var status *StatusError
	if !errors.As(err, &status) || status.Code != 404 || len(got()) != 1 {
		t.Fatalf("got %v after %d requests", err, len(got()))
	}
}

func TestNetworkErrorsNeverIncludeTheURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/ping/SECRET_TOKEN"
	srv.Close()
	err := client(url, 1).Send(context.Background(), Report{})
	if err == nil || strings.Contains(err.Error(), "SECRET_TOKEN") || !strings.Contains(err.Error(), "couldn't reach Pingstack") {
		t.Fatalf("got %v", err)
	}
}
