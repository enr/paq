package version

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
