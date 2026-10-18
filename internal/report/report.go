// Package report sends heartbeats, starts and failures to a Pingstack ping URL.
package report

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Kind is what a report says about the job.
type Kind int

const (
	Success Kind = iota
	Failure
	Start
)

func (k Kind) String() string {
	return [...]string{"success", "failure", "start"}[k]
}

// Report is one request to the ping URL.
type Report struct {
	Kind     Kind
	ExitCode *int
	Value    string
	Body     string
}

// Client posts reports, retrying network errors, 429 and 5xx responses.
type Client struct {
	URL       string
	Retries   int
	Timeout   time.Duration
	UserAgent string
	// Backoff returns the wait before retry n (1-based). Defaults to 1s, 2s, 4s...
	Backoff func(n int) time.Duration
	// AllowAnyHost lifts the https://*.pingstack.io rule; only tests set it.
	AllowAnyHost bool
}

// ErrBadURL means the ping URL isn't a Pingstack one; nothing was sent.
var ErrBadURL = errors.New("the ping URL must be an https address on pingstack.io, like https://pingstack.io/ping/YOUR_TOKEN")

// ValidateURL checks that a ping URL is https on pingstack.io or a subdomain.
func ValidateURL(raw string, allowAnyHost bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, ErrBadURL
	}
	if allowAnyHost {
		return u, nil
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" || (host != "pingstack.io" && !strings.HasSuffix(host, ".pingstack.io")) {
		return nil, ErrBadURL
	}
	return u, nil
}

// Endpoint is the URL a report goes to: the ping URL, or its /fail or /start.
func (c *Client) Endpoint(r Report) (string, error) {
	u, err := ValidateURL(c.URL, c.AllowAnyHost)
	if err != nil {
		return "", err
	}
	switch r.Kind {
	case Failure:
		u.Path = strings.TrimSuffix(u.Path, "/") + "/fail"
	case Start:
		u.Path = strings.TrimSuffix(u.Path, "/") + "/start"
	}
	q := u.Query()
	if r.ExitCode != nil {
		q.Set("exit_code", strconv.Itoa(*r.ExitCode))
	}
	if r.Value != "" {
		q.Set("value", r.Value)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// StatusError is a response Pingstack won't accept on a retry.
type StatusError struct{ Code int }

func (e *StatusError) Error() string {
	if e.Code == http.StatusNotFound {
		return "Pingstack answered 404: check that the ping URL is the check's current one"
	}
	return fmt.Sprintf("Pingstack answered %d", e.Code)
}

// Send delivers a report. Errors never include the ping URL.
func (c *Client) Send(ctx context.Context, r Report) error {
	endpoint, err := c.Endpoint(r)
	if err != nil {
		return err
	}
	backoff := c.Backoff
	if backoff == nil {
		backoff = func(n int) time.Duration { return time.Duration(1<<(n-1)) * time.Second }
	}
	client := &http.Client{Timeout: c.Timeout}

	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return lastErr
			case <-time.After(backoff(attempt)):
			}
		}
		retry, err := c.post(ctx, client, endpoint, r.Body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry || attempt >= c.Retries {
			if attempt > 0 {
				return fmt.Errorf("%w (after %d tries)", err, attempt+1)
			}
			return err
		}
	}
}

func (c *Client) post(ctx context.Context, client *http.Client, endpoint, body string) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return false, ErrBadURL
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		// url.Error repeats the URL; keep only the cause.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return true, fmt.Errorf("couldn't reach Pingstack: %v", err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return true, &StatusError{resp.StatusCode}
	default:
		return false, &StatusError{resp.StatusCode}
	}
}
