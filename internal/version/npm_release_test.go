package version

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// npmPackumentServer serves the full metadata document of "tool": every key
// of published is a version, published that long ago; latest is the
// dist-tag. "/tool/latest" serves the dist-tag's manifest.
func npmPackumentServer(t *testing.T, latest string, published map[string]time.Duration) *http.Client {
	t.Helper()
	now := time.Now()
	versions := map[string]any{}
	times := map[string]any{"created": now.Add(-1000 * time.Hour).Format(time.RFC3339), "modified": now.Format(time.RFC3339)}
	for v, ago := range published {
		versions[v] = map[string]string{"version": v}
		times[v] = now.Add(-ago).Format(time.RFC3339)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tool":
			json.NewEncoder(w).Encode(map[string]any{
				"dist-tags": map[string]string{"latest": latest},
				"versions":  versions,
				"time":      times,
			})
		case "/tool/latest":
			json.NewEncoder(w).Encode(map[string]string{"version": latest})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
}

func TestNpmReleaseProviderNoMinimumAge(t *testing.T) {
	client := npmPackumentServer(t, "2.0.0", map[string]time.Duration{"2.0.0": time.Hour})
	ver, tag, pending, err := NpmReleaseProvider{Package: "tool", HTTPClient: client}.ResolveWithPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2.0.0" || tag != "2.0.0" || pending != nil {
		t.Errorf("got %q %q %v, want 2.0.0 2.0.0 nil", ver, tag, pending)
	}
}

func TestNpmReleaseProviderLatestOldEnough(t *testing.T) {
	client := npmPackumentServer(t, "2.0.0", map[string]time.Duration{"1.9.0": 100 * time.Hour, "2.0.0": 48 * time.Hour})
	p := NpmReleaseProvider{Package: "tool", MinimumAge: 24 * time.Hour, HTTPClient: client}
	ver, _, pending, err := p.ResolveWithPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2.0.0" || pending != nil {
		t.Errorf("got %q %v, want 2.0.0 and no pending release", ver, pending)
	}
}

// TestNpmReleaseProviderLatestTooYoung verifies that a "latest" younger than
// MinimumAge falls back to the highest older stable version not above it,
// skipping prereleases and versions above the dist-tag, and is reported as
// pending.
func TestNpmReleaseProviderLatestTooYoung(t *testing.T) {
	client := npmPackumentServer(t, "2.0.1", map[string]time.Duration{
		"1.10.0":     200 * time.Hour,
		"1.9.0":      300 * time.Hour,
		"2.0.0":      48 * time.Hour,
		"2.0.1-rc.1": 30 * time.Hour,  // prerelease
		"3.0.0":      100 * time.Hour, // above the dist-tag
		"2.0.1":      time.Hour,
	})
	p := NpmReleaseProvider{Package: "tool", MinimumAge: 24 * time.Hour, HTTPClient: client}
	ver, tag, pending, err := p.ResolveWithPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2.0.0" || tag != "2.0.0" {
		t.Errorf("got %q (tag %q), want 2.0.0", ver, tag)
	}
	if pending == nil || pending.Version != "2.0.1" {
		t.Fatalf("pending = %+v, want 2.0.1", pending)
	}
	if !pending.EligibleAt.Equal(pending.PublishedAt.Add(24 * time.Hour)) {
		t.Errorf("EligibleAt = %v, want PublishedAt + 24h", pending.EligibleAt)
	}
}

func TestNpmReleaseProviderNoneEligible(t *testing.T) {
	client := npmPackumentServer(t, "1.0.0", map[string]time.Duration{"1.0.0": time.Hour})
	p := NpmReleaseProvider{Package: "tool", MinimumAge: 24 * time.Hour, HTTPClient: client}
	if _, _, err := p.Resolve(context.Background()); err == nil || !strings.Contains(err.Error(), "minimum_release_age") {
		t.Errorf("err = %v, want a minimum_release_age error", err)
	}
}
