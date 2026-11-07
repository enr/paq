package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/enr/paq/internal/httperr"
)

func TestHintFor(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	cases := []struct {
		name    string
		err     error
		wantSub string // substring expected in the hint
	}{
		{"sha256 mismatch", errors.New("integrity check failed: sha256 mismatch for x"), "corrupted or tampered"},
		{"sha512 mismatch", errors.New("integrity check failed: sha512 mismatch for x"), "corrupted or tampered"},
		{"signature", errors.New("signature verification failed: minisign signature is invalid"), "signature could not be verified"},
		{"manifest", errors.New(`app "rg" not found in manifest`), "~/.config/paq/config.toml"},
		{"registry", errors.New(`spec "rg" not found in registry`), "paq search"},
		{"unknown backend", errors.New(`resolve download URL: unknown backend: "gihub"`), `"npm"`},
		{"unsupported platform", errors.New(`"bat" is not available for linux/arm64 (supported: linux/amd64)`), "no build for your OS/architecture"},
		{"github 404", ghAPIErr(404), "if the repository is private, set the GITHUB_TOKEN"},
		{"github rate limit", ghRateLimitErr(), "set the GITHUB_TOKEN environment variable to raise the limit"},
		{"github 403 not rate limit", ghAPIErr(403), "not for a rate limit"},
		{"github 500", ghAPIErr(500), "temporary"},
		{"download 404", downloadErr("https://example.com/x.tar.gz", 404), "build exists for your platform"},
		{"github release 404", downloadErr("https://github.com/o/r/releases/download/v1/x.zip", 404), "if the repository is private, set the GITHUB_TOKEN"},
		{"npm 404", &httperr.StatusError{Code: 404, URL: "https://registry.npmjs.org/nope", Service: httperr.NpmRegistry}, "spec `package`"},
		{"other API 503", &httperr.StatusError{Code: 503, URL: "https://nodejs.org/dist/index.json", Service: "nodejs.org"}, "temporary"},
		{"download network error", errors.New("GET https://example.com/x.tar.gz: connection refused"), "--debug"},
		{"permission", errors.New("install dir: open /usr/bin/x: permission denied"), "writable"},
		{"generic", errors.New("something unexpected happened"), "--debug"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hintFor(tc.err)
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("hintFor(%q) = %q, want substring %q", tc.err, got, tc.wantSub)
			}
		})
	}
}

// TestHintForGitHub404WithToken verifies a 404 from the GitHub API with a
// token in use points at the token's access, not at setting one.
func TestHintForGitHub404WithToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "set")
	got := hintFor(ghAPIErr(404))
	if !strings.Contains(got, "token can read the repository") || strings.Contains(got, "set the GITHUB_TOKEN") {
		t.Errorf("hintFor = %q, want the token-access hint", got)
	}
}

// TestHintForGitHub403WithToken verifies a 403 with a token in use does not
// tell the user to set the token they already have.
func TestHintForGitHub403WithToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "set")
	got := hintFor(ghAPIErr(403))
	if !strings.Contains(got, "with the token in use, and not for a rate limit") || strings.Contains(got, "set the GITHUB_TOKEN") {
		t.Errorf("hintFor = %q, want the token-refused hint", got)
	}
	got = hintFor(ghRateLimitErr())
	if !strings.Contains(got, "token in use has hit its rate limit") {
		t.Errorf("hintFor(rate limit) = %q, want the token rate-limit hint", got)
	}
}

func TestHintForUsageError(t *testing.T) {
	got := hintFor(errors.New(`unknown command "bogus" for "paq"`))
	if !strings.Contains(got, "--help") {
		t.Errorf("hintFor = %q, want a pointer to --help", got)
	}
}

// ghAPIErr builds the error a GitHub API call returns, wrapped the way the
// install pipeline wraps it.
func ghAPIErr(code int) error {
	return fmt.Errorf("resolve version: %w", &httperr.StatusError{
		Code: code, URL: "https://api.github.com/repos/o/r/releases/tags/v1", Service: httperr.GitHubAPI,
	})
}

func ghRateLimitErr() error {
	return fmt.Errorf("resolve version: %w", &httperr.StatusError{
		Code: 403, URL: "https://api.github.com/repos/o/r/releases", Service: httperr.GitHubAPI, RateLimited: true,
	})
}

func downloadErr(url string, code int) error {
	return fmt.Errorf("download %s: %w", url, &httperr.StatusError{Code: code, URL: url})
}
