package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/template"
	"github.com/spf13/cobra"
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

// serveSelfUpdateLatest points selfUpdateClient at a fake GitHub API exposing
// only the "latest release" lookup (tag_name = tag). Used for runSelfUpdate's
// early-return paths (up to date / --check / ahead of latest), which must
// never reach the asset-download endpoints; any other request is counted so
// tests can assert none occurred.
func serveSelfUpdateLatest(t *testing.T, tag string) *atomic.Bool {
	t.Helper()
	var otherRequest atomic.Bool

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/enr/paq/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		otherRequest.Store(true)
		w.WriteHeader(http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	prev := selfUpdateClient
	selfUpdateClient = func() *http.Client {
		return &http.Client{Transport: &selfUpdateRewriteTransport{base: srv.URL}}
	}
	t.Cleanup(func() { selfUpdateClient = prev })

	return &otherRequest
}

// selfUpdateCmdWithFlags builds a throwaway command carrying the same
// --check/--force flags as selfUpdateCmd, so a test doesn't disturb the
// shared global command's flag state.
func selfUpdateCmdWithFlags(t *testing.T, check, force bool) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().BoolP("check", "c", false, "")
	cmd.Flags().BoolP("force", "f", false, "")
	if check {
		if err := cmd.Flags().Set("check", "true"); err != nil {
			t.Fatal(err)
		}
	}
	if force {
		if err := cmd.Flags().Set("force", "true"); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

// TestRunSelfUpdateAlreadyUpToDate verifies that a version matching the
// latest release returns cleanly without touching any download endpoint.
func TestRunSelfUpdateAlreadyUpToDate(t *testing.T) {
	withVersion(t, "1.0.0")
	otherRequest := serveSelfUpdateLatest(t, "v1.0.0")

	if err := runSelfUpdate(selfUpdateCmdWithFlags(t, false, false), nil); err != nil {
		t.Fatalf("runSelfUpdate: %v, want nil (already up to date)", err)
	}
	if otherRequest.Load() {
		t.Error("runSelfUpdate touched a download endpoint despite being up to date")
	}
}

// TestRunSelfUpdateAheadOfLatest verifies a version newer than the latest
// release (a pre-release/dev build) is reported, not treated as an error.
func TestRunSelfUpdateAheadOfLatest(t *testing.T) {
	withVersion(t, "2.0.0")
	otherRequest := serveSelfUpdateLatest(t, "v1.0.0")

	if err := runSelfUpdate(selfUpdateCmdWithFlags(t, false, false), nil); err != nil {
		t.Fatalf("runSelfUpdate: %v, want nil (ahead of the latest release is not an error)", err)
	}
	if otherRequest.Load() {
		t.Error("runSelfUpdate touched a download endpoint despite already being ahead of the latest release")
	}
}

// TestRunSelfUpdateCheckOnlyReportsAvailability verifies --check reports an
// available update without downloading or installing it.
func TestRunSelfUpdateCheckOnlyReportsAvailability(t *testing.T) {
	withVersion(t, "1.0.0")
	otherRequest := serveSelfUpdateLatest(t, "v2.0.0")

	if err := runSelfUpdate(selfUpdateCmdWithFlags(t, true, false), nil); err != nil {
		t.Fatalf("runSelfUpdate --check: %v, want nil", err)
	}
	if otherRequest.Load() {
		t.Error("--check must only report availability, never download anything")
	}
}

// TestRunSelfUpdateForceProceedsWhenUpToDate verifies --force bypasses the
// up-to-date short-circuit: it must reach the real update path (asset
// resolution) rather than silently no-opping like a plain `self-update` would.
// The fixture has no asset endpoints, so the attempt fails past that point —
// the failure itself is the proof --force did not take the early return.
func TestRunSelfUpdateForceProceedsWhenUpToDate(t *testing.T) {
	withVersion(t, "1.0.0")
	otherRequest := serveSelfUpdateLatest(t, "v1.0.0")

	err := runSelfUpdate(selfUpdateCmdWithFlags(t, false, true), nil)
	if err == nil {
		t.Fatal("expected an error once --force proceeds to a real (unfixtured) download, got nil")
	}
	if !otherRequest.Load() {
		t.Error("--force did not attempt to resolve/download release assets")
	}
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
	assertIsServedZip(t, zipPath, zipData)
}

// assertIsServedZip verifies that the path returned by downloadAndVerifyRelease
// holds the release archive and not one of the auxiliary assets downloaded
// alongside it (SHA256SUMS, .minisig): checking only the error would let a
// mixed-up return value install a text file as the new paq binary.
func assertIsServedZip(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("returned path is not readable: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("returned file = %q, want the release archive %q", got, want)
	}
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
	assertIsServedZip(t, zipPath, zipData)
}

// installSnapshot puts a minimal registry snapshot at the given version into
// the cache, without going through the network.
func installSnapshot(t *testing.T, version string) {
	t.Helper()
	staging, err := registry.StagingDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(staging, "registry")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"tool.toml": "[t]\nbackend = \"github\"\nrepo = \"owner/t\"\n",
		"VERSION":   version + "\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Install(staging, registry.Meta{Tag: "custom", Version: version, SpecCount: 1}); err != nil {
		t.Fatal(err)
	}
}

// TestRefreshRegistrySnapshotSkipsCustomSource verifies that a snapshot coming
// from a user-configured registry, which has its own version line, is left
// alone by the self-update.
func TestRefreshRegistrySnapshotSkipsCustomSource(t *testing.T) {
	s := newSigner(t)
	url := serve(t, validFixture(t, s, "1.0.0", "[t]\nbackend = \"github\"\nrepo = \"owner/t\"\n"))
	setupEnv(t, url, s.pubB64)
	installSnapshot(t, "0.9.0")

	refreshRegistrySnapshot(context.Background())

	_, meta, err := registry.Open()
	if err != nil || meta == nil {
		t.Fatalf("snapshot lost: meta=%+v err=%v", meta, err)
	}
	if meta.Version != "0.9.0" {
		t.Errorf("registry version = %q, want the custom snapshot left at 0.9.0", meta.Version)
	}
}
