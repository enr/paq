package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/enr/paq/internal/template"
)

func TestGitHubBackendResolve(t *testing.T) {
	release := map[string]any{
		"assets": []map[string]string{
			{"name": "tool-1.0-linux-amd64.tar.gz", "browser_download_url": "https://example.com/tool-1.0-linux-amd64.tar.gz"},
			{"name": "tool-1.0-darwin-arm64.tar.gz", "browser_download_url": "https://example.com/tool-1.0-darwin-arm64.tar.gz"},
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
	if url != "https://example.com/tool-1.0-linux-amd64.tar.gz" {
		t.Errorf("url = %q, want .../tool-1.0-linux-amd64.tar.gz", url)
	}
}

func TestGitHubBackendAssetNotFound(t *testing.T) {
	release := map[string]any{
		"assets": []map[string]string{
			{"name": "other-asset.tar.gz", "browser_download_url": "https://example.com/other.tar.gz"},
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

type rewriteTransport struct{ base string }

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = rt.base[len("http://"):]
	return http.DefaultTransport.RoundTrip(req2)
}
