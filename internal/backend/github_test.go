package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/enr/paq/internal/template"
)

func TestGitHubBackendResolve(t *testing.T) {
	release := map[string]any{
		"assets": []map[string]string{
			{"name": "tool-1.0-linux-amd64.tar.gz", "url": "https://api.github.com/repos/test/repo/releases/assets/1"},
			{"name": "tool-1.0-darwin-arm64.tar.gz", "url": "https://api.github.com/repos/test/repo/releases/assets/2"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	b := GitHubBackend{
		Repo:  "test/repo",
		Asset: "tool-{{version}}-{{os}}-{{arch}}.tar.gz",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{base: srv.URL},
		},
	}

	v := template.Vars{
		OS:      "linux",
		Arch:    "amd64",
		Version: "1.0",
	}

	url, err := b.Resolve(context.Background(), "v1.0", v)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://api.github.com/repos/test/repo/releases/assets/1" {
		t.Errorf("url = %q, want .../releases/assets/1", url)
	}
}

func TestGitHubBackendAssetNotFound(t *testing.T) {
	release := map[string]any{
		"assets": []map[string]string{
			{"name": "other-asset.tar.gz", "url": "https://api.github.com/repos/test/repo/releases/assets/9"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	b := GitHubBackend{
		Repo:  "test/repo",
		Asset: "tool-{{version}}-{{os}}-{{arch}}.tar.gz",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{base: srv.URL},
		},
	}

	v := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0"}
	_, err := b.Resolve(context.Background(), "v1.0", v)
	if err == nil {
		t.Error("expected error for missing asset, got nil")
	}
}

// TestGitHubBackendResolvePaginatesAssets verifies that when the release's
// embedded "assets" array is a full first page (100 entries) without a
// match, Resolve fetches further pages of /releases/{id}/assets and finds
// the asset there, forwarding the auth token.
func TestGitHubBackendResolvePaginatesAssets(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	dummyAssets := make([]map[string]string, 100)
	for i := range dummyAssets {
		dummyAssets[i] = map[string]string{
			"name": fmt.Sprintf("dummy-%d.tar.gz", i),
			"url":  fmt.Sprintf("https://api.github.com/repos/test/repo/releases/assets/%d", i),
		}
	}

	var pagedRequestAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/releases/tags/v1.0", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":     42,
			"assets": dummyAssets,
		})
	})
	mux.HandleFunc("/repos/test/repo/releases/42/assets", func(w http.ResponseWriter, r *http.Request) {
		pagedRequestAuth = r.Header.Get("Authorization")
		if r.URL.Query().Get("page") == "2" {
			json.NewEncoder(w).Encode([]map[string]string{
				{"name": "tool-1.0-linux-amd64.tar.gz", "url": "https://api.github.com/repos/test/repo/releases/assets/999"},
			})
			return
		}
		json.NewEncoder(w).Encode([]map[string]string{})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	b := GitHubBackend{
		Repo:  "test/repo",
		Asset: "tool-{{version}}-{{os}}-{{arch}}.tar.gz",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{base: srv.URL},
		},
	}

	v := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0"}
	url, err := b.Resolve(context.Background(), "v1.0", v)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://api.github.com/repos/test/repo/releases/assets/999" {
		t.Errorf("url = %q, want .../releases/assets/999", url)
	}
	if pagedRequestAuth != "Bearer test-token" {
		t.Errorf("paged request Authorization = %q, want Bearer test-token", pagedRequestAuth)
	}
}

// TestGitHubBackendResolvePaginationNotFound verifies that when the release
// has a full first page and every further page is empty, Resolve reports
// "not found" instead of erroring on pagination.
func TestGitHubBackendResolvePaginationNotFound(t *testing.T) {
	dummyAssets := make([]map[string]string, 100)
	for i := range dummyAssets {
		dummyAssets[i] = map[string]string{
			"name": fmt.Sprintf("dummy-%d.tar.gz", i),
			"url":  fmt.Sprintf("https://api.github.com/repos/test/repo/releases/assets/%d", i),
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/releases/tags/v1.0", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":     42,
			"assets": dummyAssets,
		})
	})
	mux.HandleFunc("/repos/test/repo/releases/42/assets", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]string{})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	b := GitHubBackend{
		Repo:  "test/repo",
		Asset: "tool-{{version}}-{{os}}-{{arch}}.tar.gz",
		HTTPClient: &http.Client{
			Transport: &rewriteTransport{base: srv.URL},
		},
	}

	v := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0"}
	_, err := b.Resolve(context.Background(), "v1.0", v)
	if err == nil {
		t.Error("expected error for missing asset, got nil")
	}
}

// TestGitHubBackendPaginationIsBounded verifies that an API that never returns
// a short page (e.g. one ignoring the page parameter) stops the walk with an
// error instead of issuing requests forever.
func TestGitHubBackendPaginationIsBounded(t *testing.T) {
	dummyAssets := make([]map[string]string, 100)
	for i := range dummyAssets {
		dummyAssets[i] = map[string]string{
			"name": fmt.Sprintf("dummy-%d.tar.gz", i),
			"url":  fmt.Sprintf("https://api.github.com/repos/test/repo/releases/assets/%d", i),
		}
	}

	var pages int
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/releases/tags/v1.0", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "assets": dummyAssets})
	})
	mux.HandleFunc("/repos/test/repo/releases/42/assets", func(w http.ResponseWriter, r *http.Request) {
		pages++
		json.NewEncoder(w).Encode(dummyAssets) // always a full page
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	b := GitHubBackend{
		Repo:       "test/repo",
		Asset:      "tool-{{version}}-{{os}}-{{arch}}.tar.gz",
		HTTPClient: &http.Client{Transport: &rewriteTransport{base: srv.URL}},
	}

	v := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0"}
	_, err := b.Resolve(context.Background(), "v1.0", v)
	if err == nil {
		t.Fatal("expected an error once the page cap is reached, got nil")
	}
	if want := maxAssetPages - 1; pages != want { // pages 2..maxAssetPages
		t.Errorf("fetched %d pages, want %d", pages, want)
	}
}

// serveRelease serves a single-page release with the given asset names; each
// asset's URL ends in its index.
func serveRelease(t *testing.T, names ...string) *http.Client {
	t.Helper()
	assets := make([]map[string]string, len(names))
	for i, n := range names {
		assets[i] = map[string]string{"name": n, "url": fmt.Sprintf("https://api.github.com/repos/test/repo/releases/assets/%d", i)}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "assets": assets})
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &rewriteTransport{base: srv.URL}}
}

func TestGitHubBackendResolveAssetGlob(t *testing.T) {
	b := GitHubBackend{
		Repo:       "test/repo",
		Asset:      "tool-*-{{os}}-{{arch}}.tar.gz",
		HTTPClient: serveRelease(t, "tool-1.0-darwin-arm64.tar.gz", "tool-1.0+build7-linux-amd64.tar.gz", "tool-1.0+build7-linux-amd64.tar.gz.sha256"),
	}

	v := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0"}
	url, name, err := b.ResolveAsset(context.Background(), "v1.0", v)
	if err != nil {
		t.Fatal(err)
	}
	if name != "tool-1.0+build7-linux-amd64.tar.gz" {
		t.Errorf("name = %q, want the matched asset name", name)
	}
	if url != "https://api.github.com/repos/test/repo/releases/assets/1" {
		t.Errorf("url = %q, want .../releases/assets/1", url)
	}
}

func TestGitHubBackendResolveAssetGlobAmbiguous(t *testing.T) {
	b := GitHubBackend{
		Repo:       "test/repo",
		Asset:      "tool-*-{{os}}-{{arch}}.tar.gz",
		HTTPClient: serveRelease(t, "tool-1.0-gnu-linux-amd64.tar.gz", "tool-1.0-musl-linux-amd64.tar.gz"),
	}

	v := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0"}
	_, _, err := b.ResolveAsset(context.Background(), "v1.0", v)
	if err == nil {
		t.Fatal("expected an error for a pattern matching two assets, got nil")
	}
	for _, want := range []string{"matches 2 assets", "tool-1.0-gnu-linux-amd64.tar.gz", "tool-1.0-musl-linux-amd64.tar.gz"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

func TestGitHubBackendResolveAssetInvalidPattern(t *testing.T) {
	b := GitHubBackend{Repo: "test/repo", Asset: "tool-[linux.tar.gz", HTTPClient: serveRelease(t)}
	if _, _, err := b.ResolveAsset(context.Background(), "v1.0", template.Vars{}); err == nil || !strings.Contains(err.Error(), "invalid asset pattern") {
		t.Fatalf("error = %v, want an invalid pattern error", err)
	}
}

// A glob matching on the first page must still walk the later ones: a second
// match there makes it ambiguous.
func TestGitHubBackendResolveAssetGlobWalksAllPages(t *testing.T) {
	firstPage := make([]map[string]string, 100)
	for i := range firstPage {
		firstPage[i] = map[string]string{"name": fmt.Sprintf("dummy-%d.tar.gz", i), "url": fmt.Sprintf("u%d", i)}
	}
	firstPage[0] = map[string]string{"name": "tool-1.0-linux-amd64.tar.gz", "url": "u-first"}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/releases/tags/v1.0", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "assets": firstPage})
	})
	mux.HandleFunc("/repos/test/repo/releases/42/assets", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]string{{"name": "tool-1.0-linux-amd64-musl.tar.gz", "url": "u-second"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	b := GitHubBackend{
		Repo:       "test/repo",
		Asset:      "tool-{{version}}-{{os}}-{{arch}}*.tar.gz",
		HTTPClient: &http.Client{Transport: &rewriteTransport{base: srv.URL}},
	}
	v := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0"}
	if _, _, err := b.ResolveAsset(context.Background(), "v1.0", v); err == nil || !strings.Contains(err.Error(), "matches 2 assets") {
		t.Fatalf("error = %v, want an ambiguity error", err)
	}
}

type rewriteTransport struct{ base string }

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = rt.base[len("http://"):]
	return http.DefaultTransport.RoundTrip(req2)
}
