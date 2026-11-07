// Package httperr defines the error returned when a server answers with an
// unexpected HTTP status, so callers can react to the status code and URL
// without parsing error messages.
package httperr

import (
	"fmt"
	"net/http"
)

// Service names used by the callers, for the hints that depend on them.
const (
	GitHubAPI   = "GitHub API"
	NpmRegistry = "npm registry"
)

// StatusError is an unexpected HTTP status from a server paq talked to.
type StatusError struct {
	Code int
	URL  string
	// Service names the API that answered (e.g. GitHubAPI), "" for a plain
	// file download.
	Service string
	// RateLimited is set when the refusal is a rate limit (see RateLimited),
	// as opposed to, say, a proxy or a permission denying the request.
	RateLimited bool
}

// RateLimited reports whether resp is a rate-limit refusal: a 429, or a 403
// that GitHub marks as one, with no requests left in the window (primary
// limit) or a Retry-After (secondary limit).
func RateLimited(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""
	}
	return false
}

func (e *StatusError) Error() string {
	if e.Service != "" {
		return fmt.Sprintf("%s returned %d for %s", e.Service, e.Code, e.URL)
	}
	return fmt.Sprintf("HTTP %d", e.Code)
}
