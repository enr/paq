package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/template"
)

func TestSelfUpdateAssetName(t *testing.T) {
	cases := []struct {
		tag, os, arch string
		want          string
	}{
		{"v0.1.0", "linux", "amd64", "paq-v0.1.0-linux-amd64.zip"},
		{"v1.2.3", "darwin", "arm64", "paq-v1.2.3-darwin-arm64.zip"},
		{"v0.1.0", "windows", "amd64", "paq-v0.1.0-windows-amd64.zip"},
	}
	for _, c := range cases {
		if got := selfUpdateAssetName(c.tag, c.os, c.arch); got != c.want {
			t.Errorf("selfUpdateAssetName(%q, %q, %q) = %q, want %q", c.tag, c.os, c.arch, got, c.want)
		}
	}
}

// selfUpdateRewriteTransport redirects every request to api.github.com onto
// the given test server, keeping path/query intact (mirrors the same-named
// helper in internal/backend/github_test.go, duplicated here since it's unexported there).
type selfUpdateRewriteTransport struct{ base string }

func (rt *selfUpdateRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = rt.base[len("http://"):]
	return http.DefaultTransport.RoundTrip(req2)
}

// selfUpdateFixture is a served self-update release (zip + checksums + optional signature).
type selfUpdateFixture struct {
	zip  []byte
	sums []byte
	sig  []byte // nil = asset not published
}

// serveSelfUpdate starts a fake GitHub API exposing the release-by-tag
// lookup and the three asset download endpoints, and points selfUpdateClient
// at it. Returns the tag to resolve.
func serveSelfUpdateRelease(t *testing.T, tag, assetName string, f selfUpdateFixture) {
	t.Helper()
	mux := http.NewServeMux()

	assets := []map[string]string{
		{"name": assetName, "url": "https://api.github.com/repos/enr/paq/releases/assets/1"},
		{"name": selfUpdateChecksums, "url": "https://api.github.com/repos/enr/paq/releases/assets/2"},
	}
	if f.sig != nil {
		assets = append(assets, map[string]string{"name": selfUpdateChecksumsSig, "url": "https://api.github.com/repos/enr/paq/releases/assets/3"})
	}

	mux.HandleFunc("/repos/enr/paq/releases/tags/"+tag, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"assets": assets})
	})
	mux.HandleFunc("/repos/enr/paq/releases/assets/1", func(w http.ResponseWriter, r *http.Request) { w.Write(f.zip) })
	mux.HandleFunc("/repos/enr/paq/releases/assets/2", func(w http.ResponseWriter, r *http.Request) { w.Write(f.sums) })
	if f.sig != nil {
		mux.HandleFunc("/repos/enr/paq/releases/assets/3", func(w http.ResponseWriter, r *http.Request) { w.Write(f.sig) })
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	prev := selfUpdateClient
	selfUpdateClient = func() *http.Client {
		return &http.Client{Transport: &selfUpdateRewriteTransport{base: srv.URL}}
	}
	t.Cleanup(func() { selfUpdateClient = prev })
}

func withDefaultPublicKey(t *testing.T, key string) {
	t.Helper()
	prev := registry.DefaultPublicKey
	registry.DefaultPublicKey = key
	t.Cleanup(func() { registry.DefaultPublicKey = prev })
}

// TestDownloadAndVerifyReleaseSignedSucceeds verifies that a validly signed
// SHA256SUMS lets a release-build (public key embedded) proceed.
func TestDownloadAndVerifyReleaseSignedSucceeds(t *testing.T) {
	s := newSigner(t)
	withDefaultPublicKey(t, s.pubB64)

	assetName := selfUpdateAssetName("v1.0.0", "linux", "amd64")
	zipData := []byte("fake-zip-content")
	sums := sha256Line(zipData, assetName)
	sig := s.sign(t, sums)

	serveSelfUpdateRelease(t, "v1.0.0", assetName, selfUpdateFixture{zip: zipData, sums: sums, sig: sig})

	vars := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0.0"}
	zipPath, err := downloadAndVerifyRelease(context.Background(), selfUpdateClient(), "v1.0.0", vars, assetName, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(zipPath)
}

// TestDownloadAndVerifyReleaseTamperedChecksumsFails verifies that a
// SHA256SUMS whose signature no longer matches its (tampered) content fails.
func TestDownloadAndVerifyReleaseTamperedChecksumsFails(t *testing.T) {
	s := newSigner(t)
	withDefaultPublicKey(t, s.pubB64)

	assetName := selfUpdateAssetName("v1.0.0", "linux", "amd64")
	zipData := []byte("fake-zip-content")
	sums := sha256Line(zipData, assetName)
	sig := s.sign(t, sums)
	tamperedSums := append(append([]byte{}, sums...), '\n') // signature no longer matches

	serveSelfUpdateRelease(t, "v1.0.0", assetName, selfUpdateFixture{zip: zipData, sums: tamperedSums, sig: sig})

	vars := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0.0"}
	_, err := downloadAndVerifyRelease(context.Background(), selfUpdateClient(), "v1.0.0", vars, assetName, nil)
	if err == nil {
		t.Fatal("expected an error for a tampered SHA256SUMS, got nil")
	}
}

// TestDownloadAndVerifyReleaseMissingSigAssetFails verifies that a
// release-build (public key embedded) refuses to fall back to checksum-only
// when the signature asset simply doesn't exist on the release.
func TestDownloadAndVerifyReleaseMissingSigAssetFails(t *testing.T) {
	s := newSigner(t)
	withDefaultPublicKey(t, s.pubB64)

	assetName := selfUpdateAssetName("v1.0.0", "linux", "amd64")
	zipData := []byte("fake-zip-content")
	sums := sha256Line(zipData, assetName)

	// No .sig in the fixture: the release predates signed SHA256SUMS.
	serveSelfUpdateRelease(t, "v1.0.0", assetName, selfUpdateFixture{zip: zipData, sums: sums, sig: nil})

	vars := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0.0"}
	_, err := downloadAndVerifyRelease(context.Background(), selfUpdateClient(), "v1.0.0", vars, assetName, nil)
	if err == nil {
		t.Fatal("expected an error when the signature asset is missing on a release build, got nil")
	}
}

// TestDownloadAndVerifyReleaseNoKeyChecksumOnly verifies that a dev build (no
// embedded public key) keeps today's checksum-only behavior even when a
// signature happens to be published.
func TestDownloadAndVerifyReleaseNoKeyChecksumOnly(t *testing.T) {
	withDefaultPublicKey(t, "")

	assetName := selfUpdateAssetName("v1.0.0", "linux", "amd64")
	zipData := []byte("fake-zip-content")
	sums := sha256Line(zipData, assetName)

	serveSelfUpdateRelease(t, "v1.0.0", assetName, selfUpdateFixture{zip: zipData, sums: sums, sig: nil})

	vars := template.Vars{OS: "linux", Arch: "amd64", Version: "1.0.0"}
	zipPath, err := downloadAndVerifyRelease(context.Background(), selfUpdateClient(), "v1.0.0", vars, assetName, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(zipPath)
}
