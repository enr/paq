package httpretry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// shortDelays shrinks the backoff so the tests don't wait for real seconds.
func shortDelays(t *testing.T) {
	prev := BaseDelay
	BaseDelay = time.Millisecond
	t.Cleanup(func() { BaseDelay = prev })
}

func get(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestDoRetriesServerErrors(t *testing.T) {
	shortDelays(t)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, err := Do(srv.Client(), get(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server was called %d times, want 3", got)
	}
}

func TestDoDoesNotRetryClientErrors(t *testing.T) {
	shortDelays(t)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	resp, err := Do(srv.Client(), get(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server was called %d times, want 1: a 404 is deterministic", got)
	}
}

func TestDoGivesUpAfterMaxAttempts(t *testing.T) {
	shortDelays(t)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	resp, err := Do(srv.Client(), get(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.StatusCode)
	}
	// Deliberately the literal 3, not maxAttempts: asserting against the
	// constant would make the test follow any change to it, so lowering
	// maxAttempts to 1 (removing retries entirely) would still pass.
	if got := calls.Load(); got != 3 {
		t.Errorf("server was called %d times, want 3 attempts (1 try + 2 retries)", got)
	}
}

func TestDoStopsWhenRetryAfterIsTooLong(t *testing.T) {
	shortDelays(t)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	resp, err := Do(srv.Client(), get(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := calls.Load(); got != 1 {
		t.Errorf("server was called %d times, want 1: waiting an hour is worse than failing", got)
	}
}

func TestDelayForHonorsRetryAfter(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"2"}}}
	delay, ok := delayFor(1, resp)
	if !ok || delay != 2*time.Second {
		t.Errorf("delayFor = (%v, %v), want (2s, true)", delay, ok)
	}
}
