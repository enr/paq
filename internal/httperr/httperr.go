// Package httperr defines the error returned when a server answers with an
// unexpected HTTP status, so callers can react to the status code and URL
// without parsing error messages.
package httperr

import "fmt"

// StatusError is an unexpected HTTP status from a server paq talked to.
type StatusError struct {
	Code int
	URL  string
	// GitHubAPI marks a request to the GitHub REST API (api.github.com).
	GitHubAPI bool
}

func (e *StatusError) Error() string {
	if e.GitHubAPI {
		return fmt.Sprintf("GitHub API returned %d for %s", e.Code, e.URL)
	}
	return fmt.Sprintf("HTTP %d", e.Code)
}
