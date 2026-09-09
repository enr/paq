package version

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// vsCodeUpdateAPI is the shape of the document the vscode recipe reads: the
// version sits next to the checksum, which is what keeps the two consistent.
const vsCodeUpdateAPI = `{"url":"https://example.com/code.tar.gz","name":"1.136.2",` +
	`"version":"88e44fa0e00b08f7758b4f6d05632e4fd5e4df6f","productVersion":"1.136.2",` +
	`"sha256hash":"7583b9f5d300bd6ac882160417d95bbd0978ebef04fb195844243ebd9f3ebe1c"}`

func TestJSONProviderResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(vsCodeUpdateAPI))
	}))
	defer srv.Close()

	p := JSONProvider{URL: srv.URL, Selector: "productVersion"}
	ver, tag, err := p.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "1.136.2" {
		t.Errorf("version = %q, want 1.136.2", ver)
	}
	if tag != "1.136.2" {
		t.Errorf("tag = %q, want 1.136.2", tag)
	}
}

// TestJSONProviderResolveNested verifies a version reached through a path,
// and that a leading "v" is cleaned off like every other provider does.
func TestJSONProviderResolveNested(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"channels":[{"name":"beta"},{"name":"stable","tag":"v2.5.0"}]}`))
	}))
	defer srv.Close()

	p := JSONProvider{URL: srv.URL, Selector: "channels.1.tag"}
	ver, tag, err := p.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2.5.0" {
		t.Errorf("version = %q, want 2.5.0", ver)
	}
	if tag != "v2.5.0" {
		t.Errorf("tag = %q, want the raw v2.5.0", tag)
	}
}

func TestJSONProviderErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/notjson":
			_, _ = w.Write([]byte(`<html>nope</html>`))
		default:
			_, _ = w.Write([]byte(vsCodeUpdateAPI))
		}
	}))
	defer srv.Close()

	cases := map[string]struct {
		provider JSONProvider
		want     string
	}{
		"no url":            {JSONProvider{Selector: "productVersion"}, "latest_url"},
		"no selector":       {JSONProvider{URL: srv.URL}, "latest_json"},
		"http error":        {JSONProvider{URL: srv.URL + "/missing", Selector: "productVersion"}, "404"},
		"not json":          {JSONProvider{URL: srv.URL + "/notjson", Selector: "productVersion"}, "decode"},
		"selector misses":   {JSONProvider{URL: srv.URL, Selector: "nope"}, "no key"},
		"value not string":  {JSONProvider{URL: srv.URL, Selector: "url.deeper"}, "want an object"},
		"index on a object": {JSONProvider{URL: srv.URL, Selector: "0"}, "no key"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := tc.provider.Resolve(context.Background())
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want mention of %q", err, tc.want)
			}
		})
	}
}

// TestJSONStrategyIsResolvable guards the wiring: import warns the user that
// "latest" cannot be resolved when Resolvable is false, so a strategy missing
// from it would look broken while working fine.
func TestJSONStrategyIsResolvable(t *testing.T) {
	req := LatestRequest{Strategy: "json", URL: "https://example.com/v.json", Selector: "version"}
	if !req.Resolvable() {
		t.Error("strategy json must be resolvable")
	}
	if _, isJSON := LatestProvider(req).(JSONProvider); !isJSON {
		t.Errorf("LatestProvider = %T, want JSONProvider", LatestProvider(req))
	}
}
