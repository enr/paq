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

// TestDoInvokesOnRetryForEachRetriedAttempt verifies the hook cmd/paq wires to
// ui.Debug (AUDIT-OBSERVABILITY.md M2: retries used to be invisible even with
// --debug) fires once per retry, with the attempt number and delay, and never
// fires for the attempt that ultimately succeeds or gives up.
func TestDoInvokesOnRetryForEachRetriedAttempt(t *testing.T) {
	shortDelays(t)
	prevOnRetry := OnRetry
	t.Cleanup(func() { OnRetry = prevOnRetry })

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	type call struct {
		attempt int
		status  int
		delay   time.Duration
	}
	var retries []call
	OnRetry = func(attempt int, resp *http.Response, err error, delay time.Duration) {
		if err != nil {
			t.Fatalf("OnRetry called with a transport error for an HTTP 502: %v", err)
		}
		retries = append(retries, call{attempt, resp.StatusCode, delay})
	}

	resp, err := Do(srv.Client(), get(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if len(retries) != 2 {
		t.Fatalf("OnRetry called %d time(s), want 2 (one per retried attempt): %+v", len(retries), retries)
	}
	for i, r := range retries {
		if want := i + 1; r.attempt != want {
			t.Errorf("retries[%d].attempt = %d, want %d", i, r.attempt, want)
		}
		if r.status != http.StatusBadGateway {
			t.Errorf("retries[%d].status = %d, want %d", i, r.status, http.StatusBadGateway)
		}
		if r.delay <= 0 {
			t.Errorf("retries[%d].delay = %v, want > 0", i, r.delay)
		}
	}
}

// A request that never needs a retry (first-try success) or that fails in a
// non-retryable way (404) must leave OnRetry untouched.
func TestDoDoesNotInvokeOnRetryWithoutARetry(t *testing.T) {
	shortDelays(t)
	prevOnRetry := OnRetry
	t.Cleanup(func() { OnRetry = prevOnRetry })

	called := false
	OnRetry = func(int, *http.Response, error, time.Duration) { called = true }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	resp, err := Do(srv.Client(), get(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if called {
		t.Error("OnRetry was called for a request that was never retried")
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
