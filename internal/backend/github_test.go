package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

type rewriteTransport struct{ base string }

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = rt.base[len("http://"):]
	return http.DefaultTransport.RoundTrip(req2)
}
