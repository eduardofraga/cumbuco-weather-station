package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(maxBody int64) *Client {
	c := New(2*time.Second, maxBody, "weather-test")
	c.BaseDelay = time.Millisecond
	return c
}

func TestGetSuccess(t *testing.T) {
	const body = "12345 1 2 3"
	lm := time.Date(2026, time.November, 7, 19, 2, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "weather-test" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Header().Set("Last-Modified", lm.Format(http.TimeFormat))
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	res, err := testClient(1024).Get(context.Background(), srv.URL, time.Time{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(res.Body) != body {
		t.Errorf("Body = %q, want %q", res.Body, body)
	}
	if !res.LastModified.Equal(lm) {
		t.Errorf("LastModified = %v, want %v", res.LastModified, lm)
	}
	if res.NotModified {
		t.Error("NotModified = true, want false")
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", res.StatusCode)
	}
}

func TestGetNotModified(t *testing.T) {
	lm := time.Date(2026, time.November, 7, 19, 2, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("If-Modified-Since"); got != lm.Format(http.TimeFormat) {
			t.Errorf("If-Modified-Since = %q, want %q", got, lm.Format(http.TimeFormat))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	res, err := testClient(1024).Get(context.Background(), srv.URL, lm)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !res.NotModified {
		t.Error("NotModified = false, want true")
	}
}

func TestGetRetriesTransientStatus(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	res, err := testClient(1024).Get(context.Background(), srv.URL, time.Time{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(res.Body) != "ok" {
		t.Errorf("Body = %q, want ok", res.Body)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestGetDoesNotRetryNotFound(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := testClient(1024).Get(context.Background(), srv.URL, time.Time{}); err == nil {
		t.Fatal("Get() error = nil, want error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

func TestGetRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	_, err := testClient(4).Get(context.Background(), srv.URL, time.Time{})
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("Get() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestGetHonoursContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testClient(1024).Get(ctx, srv.URL, time.Time{}); err == nil {
		t.Fatal("Get() error = nil, want cancellation error")
	}
}

func TestGetLimitsRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := testClient(1024).Get(context.Background(), srv.URL, time.Time{}); err == nil {
		t.Fatal("Get() error = nil, want redirect error")
	}
}
