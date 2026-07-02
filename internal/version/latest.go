package version

import (
	"context"
	"errors"
	"fmt"
)

// ErrLatestNotImplemented is returned when a backend does not (yet) support
// resolving the "latest" version. Callers can match it with errors.Is to tell
// "missing strategy" apart from a real network/parsing error.
var ErrLatestNotImplemented = errors.New("latest version strategy not implemented")

// LatestRequest carries the data needed to build a Provider for the "latest"
// version. It is a stable parameter for the extension point (hook point): add
// fields here when a new backend or strategy needs different coordinates (e.g.
// groupId/artifactId for Maven) without changing the signature.
type LatestRequest struct {
	Strategy string // explicit strategy; when set it takes precedence over the backend (e.g. "arch-linux")
	Backend  string // e.g. "github", "url", "maven"
	Repo     string // coordinates for the "github" backend (e.g. "BurntSushi/ripgrep")
	Source   string // coordinates for URL-based backends (e.g. Maven base URL)
	ArchPkg  string // package name in the official Arch repos (strategy "arch-linux")
}

// Resolvable indicates whether "latest" is resolvable by a real strategy/backend.
// Kept in sync with LatestProvider's logic for callers that need to know
// upfront whether "latest" will produce a version or an error (e.g. import).
func (req LatestRequest) Resolvable() bool {
	if req.Strategy != "" {
		return req.Strategy == "arch-linux"
	}
	return req.Backend == "github"
}

// LatestProvider is the extension point for the "latest version" strategy.
// It selects the Provider that fits the request.
//
// An explicit strategy (req.Strategy) takes precedence over the backend, so a
// spec can resolve "latest" from a source independent of the download channel
// (e.g. backend "url" + strategy "arch-linux"). Supported strategies:
//   - "arch-linux": latest version from the official Arch repos (ArchLinuxProvider).
//
// With no explicit strategy the backend is used. Supported backends:
//   - "github": resolves from the GitHub releases API (GitHubReleaseProvider).
//
// For any unhandled strategy/backend resolution is not implemented: it returns
// a Provider that fails with ErrLatestNotImplemented. To add support for a new
// source (e.g. "aur", Maven), implement a dedicated Provider and wire it here
// with a new case.
func LatestProvider(req LatestRequest) Provider {
	if req.Strategy != "" {
		switch req.Strategy {
		case "arch-linux":
			return ArchLinuxProvider{Pkg: req.ArchPkg}
		default:
			return notImplementedProvider{backend: req.Strategy}
		}
	}

	switch req.Backend {
	case "github":
		return GitHubReleaseProvider{Repo: req.Repo}
	default:
		return notImplementedProvider{backend: req.Backend}
	}
}

// notImplementedProvider is the fallback Provider for backends without a
// "latest version" strategy. Resolve always fails with ErrLatestNotImplemented,
// keeping the point to extend explicit.
type notImplementedProvider struct {
	backend string
}

func (p notImplementedProvider) Resolve(_ context.Context) (string, string, error) {
	return "", "", fmt.Errorf("backend %q: %w", p.backend, ErrLatestNotImplemented)
}
