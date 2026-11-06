// Package httperr defines the error returned when a server answers with an
// unexpected HTTP status, so callers can react to the status code and URL
// without parsing error messages.
package httperr

import "fmt"

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
}

func (e *StatusError) Error() string {
	if e.Service != "" {
		return fmt.Sprintf("%s returned %d for %s", e.Service, e.Code, e.URL)
	}
	return fmt.Sprintf("HTTP %d", e.Code)
}
