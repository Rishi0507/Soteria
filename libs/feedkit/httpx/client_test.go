package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func fastClient() *Client {
	c := New("test-agent")
	c.BaseDelay = time.Millisecond
	return c
}

func TestRetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "test-agent" {
			t.Errorf("missing user agent: %q", r.Header.Get("User-Agent"))
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	body, err := fastClient().Get(context.Background(), srv.URL, nil)
	if err != nil || string(body) != "ok" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if calls != 3 {
		t.Fatalf("want 3 calls got %d", calls)
	}
}

func TestHonorsRetryAfterOn429(t *testing.T) {
	var calls int32
	var firstAt, secondAt time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			firstAt = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			secondAt = time.Now()
			w.Write([]byte(`ok`))
		}
	}))
	defer srv.Close()

	if _, err := fastClient().Get(context.Background(), srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if d := secondAt.Sub(firstAt); d < 900*time.Millisecond {
		t.Fatalf("Retry-After not honored: waited only %v", d)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, `{"error":{"code":"NOT_FOUND"}}`, http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fastClient().Get(context.Background(), srv.URL, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 404 {
		t.Fatalf("want StatusError 404, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("4xx must not be retried, got %d calls", calls)
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := fastClient()
	c.MaxRetries = 2
	_, err := c.Get(context.Background(), srv.URL, nil)
	var se *StatusError
	if err == nil || !errors.As(err, &se) || se.Status != 503 {
		t.Fatalf("want wrapped 503 after give-up, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("want 3 attempts got %d", calls)
	}
}

func TestContextCancelStopsRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New("x")
	c.BaseDelay = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Get(ctx, srv.URL, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}
