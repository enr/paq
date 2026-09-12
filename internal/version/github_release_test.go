package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGitHubReleaseProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/test/repo/releases/latest" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v14.1.1"})
	}))
	defer srv.Close()

	// Sostituiamo l'URL dell'API con il server di test
	origTransport := http.DefaultTransport
	_ = origTransport

	client := &http.Client{
		Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport},
	}

	p := GitHubReleaseProvider{Repo: "test/repo", HTTPClient: client}
	ver, tag, err := p.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "14.1.1" {
		t.Errorf("version = %q, want 14.1.1", ver)
	}
	if tag != "v14.1.1" {
		t.Errorf("tag = %q, want v14.1.1", tag)
	}
}

func TestGitHubReleaseProviderMinimumAge(t *testing.T) {
	now := time.Now()
	releases := []map[string]any{
		{"tag_name": "v3.0.0", "published_at": now.Add(-1 * time.Hour), "draft": false, "prerelease": false},  // too new
		{"tag_name": "v2.9.0", "published_at": now.Add(-2 * time.Hour), "draft": false, "prerelease": true},   // too new AND prerelease
		{"tag_name": "v2.8.0", "published_at": now.Add(-48 * time.Hour), "draft": true, "prerelease": false},  // old but a draft
		{"tag_name": "v2.7.0", "published_at": now.Add(-48 * time.Hour), "draft": false, "prerelease": false}, // first eligible
		{"tag_name": "v2.6.0", "published_at": now.Add(-72 * time.Hour), "draft": false, "prerelease": false}, // also eligible, but older
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/test/repo/releases" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("page") != "1" {
			json.NewEncoder(w).Encode([]any{})
			return
		}
		json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
	p := GitHubReleaseProvider{Repo: "test/repo", MinimumAge: 24 * time.Hour, HTTPClient: client}

	ver, tag, err := p.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2.7.0" || tag != "v2.7.0" {
		t.Errorf("Resolve() = (%q, %q), want (2.7.0, v2.7.0)", ver, tag)
	}
}

func TestGitHubReleaseProviderMinimumAgePaginates(t *testing.T) {
	now := time.Now()
	old := []map[string]any{{"tag_name": "v1.5.0", "published_at": now.Add(-72 * time.Hour)}}

	// Page 1 is a full page (githubReleasesPerPage) of too-new releases;
	// the eligible release only shows up on page 2.
	page1 := make([]map[string]any, githubReleasesPerPage)
	for i := range page1 {
		page1[i] = map[string]any{
			"tag_name":     fmt.Sprintf("v9.9.%d", i),
			"published_at": now,
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/test/repo/releases" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("page") {
		case "1":
			json.NewEncoder(w).Encode(page1)
		case "2":
			json.NewEncoder(w).Encode(old)
		default:
			json.NewEncoder(w).Encode([]any{})
		}
	}))
	defer srv.Close()

	client := &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
	p := GitHubReleaseProvider{Repo: "test/repo", MinimumAge: 24 * time.Hour, HTTPClient: client}

	ver, _, err := p.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "1.5.0" {
		t.Errorf("Resolve() version = %q, want 1.5.0", ver)
	}
}

func TestGitHubReleaseProviderMinimumAgeNoneEligible(t *testing.T) {
	now := time.Now()
	releases := []map[string]any{{"tag_name": "v3.0.0", "published_at": now}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
	p := GitHubReleaseProvider{Repo: "test/repo", MinimumAge: 24 * time.Hour, HTTPClient: client}

	if _, _, err := p.Resolve(context.Background()); err == nil {
		t.Error("expected an error when no release satisfies minimum_release_age, got nil")
	}
}

// prefixRoundTripper redirige tutte le richieste verso un server di test.
type prefixRoundTripper struct {
	base  string
	inner http.RoundTripper
}

func (rt *prefixRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Sostituisci host con il server di test, mantenendo path e query
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = rt.base[len("http://"):]
	return rt.inner.RoundTrip(req2)
}
