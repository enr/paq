package version

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNodejsProviderResolve verifies that the highest LTS version wins over a
// prerelease and a Current release listed first, and over a maintenance LTS
// release that the date order puts ahead of it.
func TestNodejsProviderResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dist/index.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[
			{"version":"v27.0.0-alpha.0.0.0","lts":false},
			{"version":"v24.23.0","lts":"Krypton"},
			{"version":"v26.14.0","lts":false},
			{"version":"v26.13.0","lts":"Lithium"},
			{"version":"v24.22.2","lts":"Krypton"}
		]`))
	}))
	defer srv.Close()

	client := &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
	ver, tag, err := NodejsProvider{HTTPClient: client}.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "26.13.0" {
		t.Errorf("version = %q, want 26.13.0", ver)
	}
	if tag != "v26.13.0" {
		t.Errorf("tag = %q, want v26.13.0", tag)
	}
}

func TestNodejsProviderNoLTS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"version":"v27.0.0","lts":false}]`))
	}))
	defer srv.Close()

	client := &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
	if _, _, err := (NodejsProvider{HTTPClient: client}).Resolve(context.Background()); err == nil {
		t.Fatal("expected error for an index without LTS releases, got nil")
	}
}
