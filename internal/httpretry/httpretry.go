// Package httpretry performs HTTP requests with a bounded retry on transient
// failures (connection errors, 429 and 5xx), so a single reset connection or a
// momentary rate limit does not fail a whole install.
package httpretry

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// maxAttempts is the total number of requests issued, retries included.
	maxAttempts = 3
	// maxRetryAfter is the longest Retry-After delay worth waiting for: past
	// it, failing immediately beats blocking the CLI.
	maxRetryAfter = 30 * time.Second
)

// BaseDelay is the backoff before the first retry (doubled at each further
// attempt). A var so tests, here and in the packages that issue requests,
// don't have to wait for it.
var BaseDelay = time.Second

// Do sends req with client and retries transient failures. Only requests that
// can be replayed as-is are safe here: paq issues GETs without a body.
// The request's context bounds the wait between attempts.
func Do(client *http.Client, req *http.Request) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		resp, err := client.Do(req)
		if attempt == maxAttempts || !retryable(resp, err) {
			return resp, err
		}
		delay, ok := delayFor(attempt, resp)
		if !ok {
			return resp, err
		}
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
		}
	}
}

// retryable reports whether the outcome of an attempt is worth repeating: a
// transport error, a rate limit, or a server-side failure. Other 4xx are
// deterministic and are never retried.
func retryable(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

// delayFor returns how long to wait before the attempt following attempt, and
// whether waiting is worthwhile at all. A Retry-After header (delay-seconds
// form) wins over the exponential backoff; one asking for longer than
// maxRetryAfter, or in the HTTP-date form paq cannot honour cheaply, stops the
// retries instead.
func delayFor(attempt int, resp *http.Response) (time.Duration, bool) {
	if resp != nil {
		if v := resp.Header.Get("Retry-After"); v != "" {
			secs, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || secs < 0 {
				return 0, false
			}
			delay := time.Duration(secs) * time.Second
			if delay > maxRetryAfter {
				return 0, false
			}
			return delay, true
		}
	}
	return BaseDelay << (attempt - 1), true
}
