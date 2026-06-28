package version

import (
	"context"
	"errors"
	"testing"
)

func TestLatestProviderGitHub(t *testing.T) {
	p := LatestProvider(LatestRequest{Backend: "github", Repo: "BurntSushi/ripgrep"})
	gh, ok := p.(GitHubReleaseProvider)
	if !ok {
		t.Fatalf("expected GitHubReleaseProvider, got %T", p)
	}
	if gh.Repo != "BurntSushi/ripgrep" {
		t.Fatalf("expected repo to be propagated, got %q", gh.Repo)
	}
}

func TestLatestProviderNotImplemented(t *testing.T) {
	for _, backend := range []string{"url", "maven", "", "unknown"} {
		p := LatestProvider(LatestRequest{Backend: backend})
		_, _, err := p.Resolve(context.Background())
		if !errors.Is(err, ErrLatestNotImplemented) {
			t.Fatalf("backend %q: expected ErrLatestNotImplemented, got %v", backend, err)
		}
	}
}

func TestLatestProviderArchLinux(t *testing.T) {
	// The explicit strategy takes precedence over the backend.
	p := LatestProvider(LatestRequest{Strategy: "arch-linux", Backend: "url", ArchPkg: "ripgrep"})
	arch, ok := p.(ArchLinuxProvider)
	if !ok {
		t.Fatalf("expected ArchLinuxProvider, got %T", p)
	}
	if arch.Pkg != "ripgrep" {
		t.Fatalf("expected arch_pkg to be propagated, got %q", arch.Pkg)
	}
}

func TestLatestProviderUnknownStrategy(t *testing.T) {
	p := LatestProvider(LatestRequest{Strategy: "nope", Backend: "github", Repo: "x/y"})
	_, _, err := p.Resolve(context.Background())
	if !errors.Is(err, ErrLatestNotImplemented) {
		t.Fatalf("expected ErrLatestNotImplemented for unknown strategy, got %v", err)
	}
}

func TestLatestRequestResolvable(t *testing.T) {
	cases := []struct {
		req  LatestRequest
		want bool
	}{
		{LatestRequest{Backend: "github", Repo: "x/y"}, true},
		{LatestRequest{Strategy: "arch-linux", ArchPkg: "rg"}, true},
		{LatestRequest{Backend: "url"}, false},
		{LatestRequest{Strategy: "nope", Backend: "github"}, false},
		{LatestRequest{}, false},
	}
	for _, c := range cases {
		if got := c.req.Resolvable(); got != c.want {
			t.Errorf("Resolvable(%+v) = %v, want %v", c.req, got, c.want)
		}
	}
}
