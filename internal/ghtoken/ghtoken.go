// Package ghtoken picks the GitHub token sent with a request: the one a spec
// names for its repo, else GITHUB_TOKEN.
package ghtoken

import (
	"os"
	"strings"
)

// byRepo maps a lowercased "owner/name" to its own token.
var byRepo = map[string]string{}

// Set makes token the one sent for repo ("owner/name"), instead of GITHUB_TOKEN.
func Set(repo, token string) {
	byRepo[strings.ToLower(repo)] = token
}

// For returns the token for repo: the one registered with Set, else
// GITHUB_TOKEN (empty when neither is set).
func For(repo string) string {
	if token, ok := byRepo[strings.ToLower(repo)]; ok {
		return token
	}
	return os.Getenv("GITHUB_TOKEN")
}

// RepoFrom returns the "owner/name" that follows "api.github.com/repos/" or
// "github.com/" in s (a URL, or an error message holding one), or "".
func RepoFrom(s string) string {
	for _, prefix := range []string{"api.github.com/repos/", "github.com/"} {
		if _, rest, ok := strings.Cut(s, prefix); ok {
			parts := strings.SplitN(rest, "/", 3)
			if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
				return ""
			}
			return parts[0] + "/" + parts[1]
		}
	}
	return ""
}
