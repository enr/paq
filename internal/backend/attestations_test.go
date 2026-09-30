package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/snappy"
)

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// attestationServer serves the attestations API (host api.github.com) and a
// blob store (host blobs.example) through rewriteTransport, recording the
// Authorization header each host received.
func attestationServer(t *testing.T, apiStatus int, apiBody string) (*http.Client, map[string]string) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "secret-token")
	auth := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth[r.Host] = r.Header.Get("Authorization")
		switch r.Host {
		case "api.github.com":
			want := "/repos/owner/tool/attestations/sha256:" + testDigest
			if r.URL.Path != want || r.URL.Query().Get("predicate_type") != SLSAProvenanceV1 {
				t.Errorf("request %s, want path %s with the SLSA predicate type", r.URL, want)
			}
			w.WriteHeader(apiStatus)
			fmt.Fprint(w, apiBody)
		case "blobs.example":
			w.Write(snappy.Encode(nil, []byte(`{"from":"bundle_url"}`)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &rewriteTransport{base: srv.URL}}, auth
}

func TestFetchAttestationBundles(t *testing.T) {
	client, auth := attestationServer(t, http.StatusOK, `{"attestations":[
		{"bundle":{"from":"inline"}},
		{"bundle":null,"bundle_url":"https://blobs.example/b1"},
		{"bundle":null}
	]}`)

	bundles, err := FetchAttestationBundles(context.Background(), client, "owner/tool", testDigest)
	if err != nil {
		t.Fatalf("FetchAttestationBundles: %v", err)
	}
	var got []string
	for _, b := range bundles {
		got = append(got, strings.Join(strings.Fields(string(b)), ""))
	}
	if want := `{"from":"inline"} {"from":"bundle_url"}`; strings.Join(got, " ") != want {
		t.Errorf("bundles = %v, want %s", got, want)
	}
	if auth["api.github.com"] != "Bearer secret-token" {
		t.Errorf("API request Authorization = %q, want the token", auth["api.github.com"])
	}
	if auth["blobs.example"] != "" {
		t.Errorf("bundle_url request Authorization = %q, want none (token must not leave GitHub)", auth["blobs.example"])
	}
}

// TestFetchAttestationBundlesNone verifies that a 404 or an empty list yields
// no bundles and no error: "no attestation" is a verdict for the caller.
func TestFetchAttestationBundlesNone(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"404":        {http.StatusNotFound, `{"message":"Not Found"}`},
		"empty list": {http.StatusOK, `{"attestations":[]}`},
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := attestationServer(t, tc.status, tc.body)
			bundles, err := FetchAttestationBundles(context.Background(), client, "owner/tool", testDigest)
			if err != nil || len(bundles) != 0 {
				t.Errorf("got %d bundles, err %v; want none and nil", len(bundles), err)
			}
		})
	}
}

// TestFetchAttestationBundlesAPIError verifies that other API failures are
// errors (the check could not be performed).
func TestFetchAttestationBundlesAPIError(t *testing.T) {
	client, _ := attestationServer(t, http.StatusForbidden, `{"message":"rate limited"}`)
	if _, err := FetchAttestationBundles(context.Background(), client, "owner/tool", testDigest); err == nil {
		t.Error("expected an error for HTTP 403, got nil")
	}
}
