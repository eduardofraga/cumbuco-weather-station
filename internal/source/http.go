// Package source provides a small HTTP client tuned for polling a single,
// slowly changing endpoint: bounded bodies, conditional requests, and retries
// for transient failures only.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"
)

// ErrBodyTooLarge is returned when a response exceeds MaxBody.
var ErrBodyTooLarge = errors.New("response body exceeds limit")

// Result is a successful response.
type Result struct {
	Body         []byte
	NotModified  bool
	LastModified time.Time
	FetchedAt    time.Time
	StatusCode   int
}

// Client fetches bytes over HTTP with retry and body limits.
type Client struct {
	HTTP      *http.Client
	MaxBody   int64
	UserAgent string
	Attempts  int
	BaseDelay time.Duration
}

// New returns a Client with sane defaults. A zero or negative timeout falls
// back to 10 seconds.
func New(timeout time.Duration, maxBody int64, userAgent string) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		HTTP: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				// One redirect is tolerated; anything beyond is rejected.
				if len(via) >= 2 {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		MaxBody:   maxBody,
		UserAgent: userAgent,
		Attempts:  3,
		BaseDelay: 200 * time.Millisecond,
	}
}

// Get performs a conditional GET. lastModified may be zero on the first call.
func (c *Client) Get(ctx context.Context, url string, lastModified time.Time) (Result, error) {
	attempts := c.Attempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, c.backoff(attempt)); err != nil {
				return Result{}, err
			}
		}

		res, err := c.once(ctx, url, lastModified)
		if err == nil {
			return res, nil
		}
		lastErr = err

		var retryable retryError
		if !errors.As(err, &retryable) || !retryable.retry {
			return Result{}, err
		}
	}
	return Result{}, fmt.Errorf("after %d attempts: %w", attempts, lastErr)
}

func (c *Client) once(ctx context.Context, url string, lastModified time.Time) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "*/*")
	if !lastModified.IsZero() {
		req.Header.Set("If-Modified-Since", lastModified.UTC().Format(http.TimeFormat))
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, retryError{err: fmt.Errorf("http get: %w", err), retry: true}
	}
	defer resp.Body.Close()

	now := time.Now().UTC()
	lm := time.Time{}
	if v := resp.Header.Get("Last-Modified"); v != "" {
		if parsed, perr := http.ParseTime(v); perr == nil {
			lm = parsed.UTC()
		}
	}

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return Result{NotModified: true, LastModified: lm, FetchedAt: now, StatusCode: resp.StatusCode}, nil
	case resp.StatusCode == http.StatusOK:
		body, err := readLimited(resp.Body, c.MaxBody)
		if err != nil {
			return Result{}, err
		}
		return Result{Body: body, LastModified: lm, FetchedAt: now, StatusCode: resp.StatusCode}, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return Result{}, retryError{
			err:   fmt.Errorf("unexpected status %d", resp.StatusCode),
			retry: true,
		}
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return Result{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		max = 2 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > max {
		return nil, ErrBodyTooLarge
	}
	return body, nil
}

func (c *Client) backoff(attempt int) time.Duration {
	base := c.BaseDelay
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 3
	}
	// Full jitter keeps retries from synchronising across instances.
	return d + time.Duration(rand.Int64N(int64(d/2)+1))
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type retryError struct {
	err   error
	retry bool
}

func (e retryError) Error() string { return e.err.Error() }
func (e retryError) Unwrap() error { return e.err }
