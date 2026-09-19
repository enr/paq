package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/version"
)

// makeFakeTarGz creates an in-memory .tar.gz with a single "rg" file containing content.
func makeFakeTarGz(content []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "ripgrep-0.1.0-x86_64-unknown-linux-gnu/rg", Mode: 0755, Size: int64(len(content))})
	tw.Write(content)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func sha512hex(data []byte) string {
	h := sha512.Sum512(data)
	return hex.EncodeToString(h[:])
}

// makeFakeZip creates an in-memory zip with a single file nested under a
// top-level directory (like maven archives), to test strip_components.
func makeFakeZip(topDir, name string, content []byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(topDir + "/" + name)
	w.Write(content)
	zw.Close()
	return buf.Bytes()
}

// isolateState redirects paq's state file to a temp directory, so tests
// running Run() don't write to the user's real state.
func isolateState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// TestPipelineSHA512URLBackend verifies installation via the "url" backend
// with sha512 verification from a "bare hash" checksum file (Apache Maven layout).
func TestPipelineSHA512URLBackend(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-mvn-binary")
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", fileContent)
	checksum := sha512hex(zipData) // bare hash, no filename

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(checksum + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify: config.VerifyConfig{
					SHA512Asset: "{{asset}}.sha512",
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {
				Use:     "maven",
				Version: "1.0.0",
				Dest:    dest,
			},
		},
	}

	if err := Run(context.Background(), cfg, "maven", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "mvn"))
	if err != nil {
		t.Fatalf("installed file not found: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("installed content = %q, want %q", data, fileContent)
	}
}

// TestPipelineOmittedVersionUsesDefault verifies that an app with NO version
// installs the spec's default_version (stable channel, no network).
func TestPipelineOmittedVersionUsesDefault(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-mvn-binary")
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", fileContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Write(zipData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				DefaultVersion:  "1.0.0",
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Dest: dest}, // version omessa → default_version
		},
	}

	if err := Run(context.Background(), cfg, "maven", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "mvn"))
	if err != nil {
		t.Fatalf("installed file not found: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("installed content = %q, want %q", data, fileContent)
	}
}

// TestPipelineOmittedDestUsesDefaults verifies that an app with NO dest
// derives its destination from the base directories configured in [defaults].
func TestPipelineOmittedDestUsesDefaults(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-mvn-binary")
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", fileContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Write(zipData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	optDir := t.TempDir()
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				DefaultVersion:  "1.0.0",
			},
		},
		Defaults: config.Defaults{Opt: optDir},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven"}, // neither version nor dest
		},
	}

	if err := Run(context.Background(), cfg, "maven", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	// derived dest = <opt>/maven (directory-style spec).
	data, err := os.ReadFile(filepath.Join(optDir, "maven", "bin", "mvn"))
	if err != nil {
		t.Fatalf("installed file not found at default dest: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("installed content = %q, want %q", data, fileContent)
	}
}

// TestPipelineLatestNoStrategyErrors verifies that version="latest" on a
// spec with no real strategy (backend "url") fails, without falling back to
// a default_version.
func TestPipelineLatestNoStrategyErrors(t *testing.T) {
	isolateState(t)
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend: "url",
				Source:  "https://example.com/{{version}}.zip",
				Archive: "zip",
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "latest", Dest: filepath.Join(t.TempDir(), "tool")},
		},
	}

	err := Run(context.Background(), cfg, "tool", nil, nil)
	if !errors.Is(err, version.ErrLatestNotImplemented) {
		t.Fatalf("expected ErrLatestNotImplemented, got %v", err)
	}
}

// TestPipelineAssetTemplateErrorSurfaces verifies that a spec.Asset template
// referencing an unknown placeholder fails the install instead of silently
// falling back to the URL's basename.
func TestPipelineAssetTemplateErrorSurfaces(t *testing.T) {
	isolateState(t)
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend: "url",
				Source:  "https://example.com/tool-{{version}}.tar.gz",
				Asset:   "tool-{{bogus}}.tar.gz",
				Archive: "tar.gz",
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: filepath.Join(t.TempDir(), "tool")},
		},
	}

	err := Run(context.Background(), cfg, "tool", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "resolve asset name") {
		t.Fatalf("expected asset name resolution error, got %v", err)
	}
}

// TestPipelineRefusesToReplaceUnownedDest verifies that installing a "dir"
// kind spec over a pre-existing, non-empty directory that paq didn't create
// fails instead of silently wiping it out, and that Force overrides the guard.
func TestPipelineRefusesToReplaceUnownedDest(t *testing.T) {
	isolateState(t)
	fileContent := []byte("payload")
	zipData := makeFakeZip("tool-1.0.0", "bin/tool", fileContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "tool")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "unrelated-file"), []byte("pre-existing"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend:         "url",
				Source:          srv.URL + "/tool-{{version}}.zip",
				Archive:         "zip",
				StripComponents: 1,
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: dest},
		},
	}

	err := Run(context.Background(), cfg, "tool", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not created by paq") {
		t.Fatalf("expected 'not created by paq' error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "unrelated-file")); err != nil {
		t.Errorf("pre-existing file was removed: %v", err)
	}

	// Force overrides the guard.
	if err := Run(context.Background(), cfg, "tool", nil, &Hooks{Force: true}); err != nil {
		t.Fatalf("install with Force failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatalf("installed file not found: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("content = %q, want %q", data, fileContent)
	}
}

// TestPipelineReinstallOwnedDestSucceedsWithoutForce verifies that a dest
// already recorded in state from a previous install (a different version)
// can be reinstalled into without --force: the upgrade/reinstall flow is
// unaffected by the "not created by paq" guard.
func TestPipelineReinstallOwnedDestSucceedsWithoutForce(t *testing.T) {
	isolateState(t)
	zipDataV1 := makeFakeZip("tool-1.0.0", "bin/tool", []byte("v1"))
	zipDataV2 := makeFakeZip("tool-2.0.0", "bin/tool", []byte("v2"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "2.0.0") {
			w.Write(zipDataV2)
			return
		}
		w.Write(zipDataV1)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend:         "url",
				Source:          srv.URL + "/tool-{{version}}.zip",
				Archive:         "zip",
				StripComponents: 1,
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: dest},
		},
	}

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("first install failed: %v", err)
	}

	cfg.Apps["tool"] = config.AppEntry{Use: "tool", Version: "2.0.0", Dest: dest}
	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("reinstall over owned dest failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatalf("installed file not found: %v", err)
	}
	if string(data) != "v2" {
		t.Errorf("content = %q, want v2", data)
	}
}

// TestPipelineSHA512Mismatch verifies that a wrong sha512 checksum makes the
// install fail without creating the destination.
func TestPipelineSHA512Mismatch(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", []byte("payload"))
	wrong := strings.Repeat("0", 128)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(wrong + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify:          config.VerifyConfig{SHA512Asset: "{{asset}}.sha512"},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	if err := Run(context.Background(), cfg, "maven", nil, nil); err == nil {
		t.Error("expected error for sha512 mismatch, got nil")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("dest should not exist after failed install")
	}
}

// TestPipelineWarnsWhenNoVerify verifies that the pipeline emits a warning
// (OnWarn hook) when the spec configures no verification.
func TestPipelineWarnsWhenNoVerify(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", []byte("payload"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Write(zipData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				// Nessun blocco Verify.
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	var warnings []string
	hooks := &Hooks{OnWarn: func(msg string) { warnings = append(warnings, msg) }}

	if err := Run(context.Background(), cfg, "maven", nil, hooks); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning when no verification is configured, got none")
	}
	if !strings.Contains(warnings[0], "no verification") {
		t.Errorf("unexpected warning message: %q", warnings[0])
	}
}

// TestPipelineNoWarnWhenVerifyConfigured verifies that the warning is NOT
// emitted when verification is configured.
func TestPipelineNoWarnWhenVerifyConfigured(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", []byte("payload"))
	checksum := sha512hex(zipData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(checksum + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify:          config.VerifyConfig{SHA512Asset: "{{asset}}.sha512"},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	var warnings []string
	hooks := &Hooks{OnWarn: func(msg string) { warnings = append(warnings, msg) }}

	if err := Run(context.Background(), cfg, "maven", nil, hooks); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warning when verification is configured, got: %v", warnings)
	}
}

func TestPipelineInstallFile(t *testing.T) {
	isolateState(t)
	binaryContent := []byte("fake-rg-binary")
	tgzData := makeFakeTarGz(binaryContent)
	assetName := "ripgrep-0.1.0-x86_64-unknown-linux-gnu.tar.gz"
	checksum := sha256hex(tgzData)
	checksumFile := fmt.Sprintf("%s  %s\n", checksum, assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "releases/latest"):
			json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.1.0"})
		case strings.Contains(r.URL.Path, "releases/tags"):
			json.NewEncoder(w).Encode(map[string]any{
				"assets": []map[string]string{
					{
						"name": assetName,
						"url":  "http://" + r.Host + "/download/" + assetName,
					},
					{
						"name": assetName + ".sha256",
						"url":  "http://" + r.Host + "/download/" + assetName + ".sha256",
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			w.Write([]byte(checksumFile))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			w.Write(tgzData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rg")

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"ripgrep": {
				Backend:           "github",
				Repo:              "test/ripgrep",
				Asset:             "ripgrep-{{version}}-x86_64-unknown-linux-gnu.tar.gz",
				Archive:           "tar.gz",
				Extract:           "rg",
				Chmod:             "0755",
				MinimumReleaseAge: "0h", // not what this test exercises
				Verify: config.VerifyConfig{
					SHA256Asset: "{{asset}}.sha256",
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"rg": {
				Use:     "ripgrep",
				Version: "latest",
				Dest:    dest,
			},
		},
	}

	// Patch: the GitHub API must point to the test server.
	// For simplicity, we modify the spec using the "url" backend
	// and test the pipeline with a GitHub API mock via transport.
	// We use a more direct approach: monkey-patch the HTTP client.
	// The test uses a custom transport that redirects to the test server.
	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{base: srv.URL, inner: origTransport}
	defer func() { http.DefaultTransport = origTransport }()

	err := Run(context.Background(), cfg, "rg", nil, nil)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Verify that the file was installed.
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("dest not found: %v", err)
	}
	if !bytes.Equal(data, binaryContent) {
		t.Errorf("dest content = %q, want %q", data, binaryContent)
	}
}

// TestPipelineMinimumReleaseAgeDefaultAppliesToGitHub verifies the built-in
// default (24h, applied even with no configuration at all): a "latest"
// install against a github-backed spec skips a release published within the
// last 24h and installs the newest one that is old enough instead, going
// through the paginated /releases listing (not /releases/latest).
func TestPipelineMinimumReleaseAgeDefaultAppliesToGitHub(t *testing.T) {
	isolateState(t)
	binaryContent := []byte("fake-rg-binary-0.1.0")
	tgzData := makeFakeTarGz(binaryContent)
	assetName := "ripgrep-0.1.0-x86_64-unknown-linux-gnu.tar.gz"
	checksum := sha256hex(tgzData)
	checksumFile := fmt.Sprintf("%s  %s\n", checksum, assetName)
	now := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases") && r.URL.Query().Get("page") == "1":
			json.NewEncoder(w).Encode([]map[string]any{
				{"tag_name": "v0.2.0", "published_at": now.Add(-1 * time.Hour), "draft": false, "prerelease": false},  // too new: <24h
				{"tag_name": "v0.1.0", "published_at": now.Add(-48 * time.Hour), "draft": false, "prerelease": false}, // eligible
			})
		case strings.HasSuffix(r.URL.Path, "/releases"):
			json.NewEncoder(w).Encode([]map[string]any{}) // no further pages
		case strings.Contains(r.URL.Path, "releases/latest"):
			t.Error("must not call /releases/latest when the default minimum age is in effect")
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "releases/tags"):
			json.NewEncoder(w).Encode(map[string]any{
				"assets": []map[string]string{
					{"name": assetName, "url": "http://" + r.Host + "/download/" + assetName},
					{"name": assetName + ".sha256", "url": "http://" + r.Host + "/download/" + assetName + ".sha256"},
				},
			})
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			w.Write([]byte(checksumFile))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			w.Write(tgzData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rg")

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			// No MinimumReleaseAge set: must fall back to the built-in 24h default.
			"ripgrep": {
				Backend: "github",
				Repo:    "test/ripgrep",
				Asset:   "ripgrep-{{version}}-x86_64-unknown-linux-gnu.tar.gz",
				Archive: "tar.gz",
				Extract: "rg",
				Verify:  config.VerifyConfig{SHA256Asset: "{{asset}}.sha256"},
			},
		},
		Apps: map[string]config.AppEntry{
			"rg": {Use: "ripgrep", Version: "latest", Dest: dest},
		},
	}

	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{base: srv.URL, inner: origTransport}
	defer func() { http.DefaultTransport = origTransport }()

	if err := Run(context.Background(), cfg, "rg", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("dest not found: %v", err)
	}
	if !bytes.Equal(data, binaryContent) {
		t.Errorf("dest content = %q, want %q (the older, eligible release)", data, binaryContent)
	}
}

// TestPipelineMinimumReleaseAgeInvalidFailsFast verifies that a malformed
// minimum_release_age on the spec fails the install before any network call,
// with an error naming the field.
func TestPipelineMinimumReleaseAgeInvalidFailsFast(t *testing.T) {
	isolateState(t)
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"ripgrep": {
				Backend:           "github",
				Repo:              "test/ripgrep",
				MinimumReleaseAge: "not-a-duration",
			},
		},
		Apps: map[string]config.AppEntry{
			"rg": {Use: "ripgrep", Version: "latest", Dest: filepath.Join(t.TempDir(), "rg")},
		},
	}

	err := Run(context.Background(), cfg, "rg", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid minimum_release_age") {
		t.Fatalf("expected an invalid minimum_release_age error, got %v", err)
	}
}

// TestPipelineMinimumReleaseAgeWarnsOnUnsupportedBackend verifies that an
// explicit minimum_release_age on a backend that can't honor it (here,
// "url", which has no per-release publish dates) warns via the OnWarn hook.
// Backend "url" with no latest_strategy also has no way at all to resolve
// "latest", so the install fails for that unrelated reason (no network
// involved either way) - what this test checks is that the warning still
// fires before that failure.
func TestPipelineMinimumReleaseAgeWarnsOnUnsupportedBackend(t *testing.T) {
	isolateState(t)
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:           "url",
				Source:            "https://example.invalid/{{version}}.zip",
				MinimumReleaseAge: "7d",
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "latest", Dest: filepath.Join(t.TempDir(), "maven")},
		},
	}

	var warnings []string
	err := Run(context.Background(), cfg, "maven", nil, &Hooks{
		OnWarn: func(msg string) { warnings = append(warnings, msg) },
	})
	if err == nil {
		t.Fatal("expected an error (backend \"url\" cannot resolve \"latest\"), got nil")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "minimum_release_age") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a minimum_release_age warning, got %v", warnings)
	}
}

func TestPipelineChecksumMismatch(t *testing.T) {
	isolateState(t)
	binaryContent := []byte("fake-rg-binary")
	tgzData := makeFakeTarGz(binaryContent)
	assetName := "ripgrep-0.1.0-x86_64-unknown-linux-gnu.tar.gz"
	// Deliberately wrong checksum.
	wrongChecksum := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	checksumFile := fmt.Sprintf("%s  %s\n", wrongChecksum, assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "releases/latest"):
			json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.1.0"})
		case strings.Contains(r.URL.Path, "releases/tags"):
			json.NewEncoder(w).Encode(map[string]any{
				"assets": []map[string]string{
					{
						"name": assetName,
						"url":  "http://" + r.Host + "/download/" + assetName,
					},
					{
						"name": assetName + ".sha256",
						"url":  "http://" + r.Host + "/download/" + assetName + ".sha256",
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			w.Write([]byte(checksumFile))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			w.Write(tgzData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rg")

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"ripgrep": {
				Backend:           "github",
				Repo:              "test/ripgrep",
				Asset:             "ripgrep-{{version}}-x86_64-unknown-linux-gnu.tar.gz",
				Archive:           "tar.gz",
				Extract:           "rg",
				MinimumReleaseAge: "0h", // not what this test exercises
				Verify: config.VerifyConfig{
					SHA256Asset: "{{asset}}.sha256",
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"rg": {
				Use:     "ripgrep",
				Version: "latest",
				Dest:    dest,
			},
		},
	}

	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{base: srv.URL, inner: origTransport}
	defer func() { http.DefaultTransport = origTransport }()

	err := Run(context.Background(), cfg, "rg", nil, nil)
	if err == nil {
		t.Error("expected error for checksum mismatch, got nil")
	}

	// dest must NOT have been created.
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("dest should not exist after failed install")
	}
}

// TestPipelineErrorShownOnceAndMarked verifies that, on error, the OnFail
// hook is invoked exactly once and the returned error is marked as
// "already shown" (so the caller doesn't reprint it).
func TestPipelineErrorShownOnceAndMarked(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", []byte("payload"))
	wrong := strings.Repeat("0", 128)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(wrong + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify:          config.VerifyConfig{SHA512Asset: "{{asset}}.sha512"},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	var failCount int
	var failErr error
	hooks := &Hooks{OnFail: func(err error) { failCount++; failErr = err }}

	err := Run(context.Background(), cfg, "maven", nil, hooks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if failCount != 1 {
		t.Errorf("OnFail called %d times, want 1", failCount)
	}
	if failErr == nil || !strings.Contains(failErr.Error(), "sha512 mismatch") {
		t.Errorf("OnFail received unexpected error: %v", failErr)
	}
	if !ErrAlreadyShown(err) {
		t.Error("returned error should be marked as already shown")
	}
}

// TestPipelineDebugHook verifies that the pipeline emits debug traces.
func TestPipelineDebugHook(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-mvn")
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", fileContent)
	checksum := sha512hex(zipData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(checksum + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify:          config.VerifyConfig{SHA512Asset: "{{asset}}.sha512"},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	var debugLines []string
	hooks := &Hooks{OnDebug: func(msg string) { debugLines = append(debugLines, msg) }}

	if err := Run(context.Background(), cfg, "maven", nil, hooks); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(debugLines) == 0 {
		t.Fatal("expected debug output, got none")
	}
	joined := strings.Join(debugLines, "\n")
	for _, want := range []string{"asset name", "sha512 checksum URL", "artifact sha256"} {
		if !strings.Contains(joined, want) {
			t.Errorf("debug output missing %q; got:\n%s", want, joined)
		}
	}
}

type redirectTransport struct {
	base  string
	inner http.RoundTripper
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = strings.TrimPrefix(rt.base, "http://")
	return rt.inner.RoundTrip(req2)
}

// TestPipelineMinisignWithoutSHA256AssetFails verifies that a spec configuring
// minisign without sha256_asset is rejected before any network access: the
// signature is verified against the checksum file, so without it the check
// would otherwise be silently skipped while looking enabled.
func TestPipelineMinisignWithoutSHA256AssetFails(t *testing.T) {
	isolateState(t)
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend: "url",
				Source:  "https://unreachable.invalid/tool-{{version}}.tar.gz",
				Archive: "tar.gz",
				Verify: config.VerifyConfig{
					Minisign: config.MinisignConfig{
						PublicKey:   "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
						SignedAsset: "{{asset}}.minisig",
					},
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: t.TempDir()},
		},
	}

	err := Run(context.Background(), cfg, "tool", nil, nil)
	if err == nil {
		t.Fatal("expected error for minisign without sha256_asset, got nil")
	}
	if !strings.Contains(err.Error(), "sha256_asset") {
		t.Errorf("error = %q, want mention of sha256_asset", err)
	}
}

// TestPipelineHalfConfiguredMinisignFails verifies that setting only one of
// public_key/signed_asset is rejected instead of being silently ignored.
func TestPipelineHalfConfiguredMinisignFails(t *testing.T) {
	isolateState(t)
	for name, ms := range map[string]config.MinisignConfig{
		"only public_key":   {PublicKey: "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3"},
		"only signed_asset": {SignedAsset: "{{asset}}.minisig"},
	} {
		cfg := &config.Config{
			Specs: map[string]config.Spec{
				"tool": {
					Backend: "url",
					Source:  "https://unreachable.invalid/tool-{{version}}.tar.gz",
					Archive: "tar.gz",
					Verify: config.VerifyConfig{
						SHA256Asset: "{{asset}}.sha256",
						Minisign:    ms,
					},
				},
			},
			Apps: map[string]config.AppEntry{
				"tool": {Use: "tool", Version: "1.0.0", Dest: t.TempDir()},
			},
		}

		err := Run(context.Background(), cfg, "tool", nil, nil)
		if err == nil {
			t.Fatalf("%s: expected error, got nil", name)
		}
		if !strings.Contains(err.Error(), "public_key and signed_asset") {
			t.Errorf("%s: error = %q, want mention of public_key and signed_asset", name, err)
		}
	}
}

// TestPipelineRecordsInstalledState verifies the state record written by the
// last step of the pipeline. These are the fields uninstall relies on to find
// what was installed: nothing asserted them, so a wrong Kind or an empty Files
// list produced an install that uninstall could no longer clean up.
func TestPipelineRecordsInstalledState(t *testing.T) {
	isolateState(t)
	zipData := makeMultiBinZip("zipp-0.8.1_linux_amd64", map[string][]byte{
		"zipts": []byte("ts"),
		"zipls": []byte("ls"),
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	binDir := t.TempDir()
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"zipp": {
				Backend:         "url",
				Source:          srv.URL + "/zipp-{{version}}.zip",
				Archive:         "zip",
				StripComponents: 1,
				Binaries:        []config.Binary{{From: "zipts"}, {From: "zipls"}},
			},
		},
		Apps: map[string]config.AppEntry{
			"zipp": {Use: "zipp", Version: "0.8.1", Dest: binDir},
		},
	}

	if err := Run(context.Background(), cfg, "zipp", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	rec, ok := st.Get("zipp", "0.8.1")
	if !ok {
		t.Fatalf("no state record for zipp 0.8.1; state = %+v", st.Packages)
	}
	if rec.Kind != "binaries" {
		t.Errorf("Kind = %q, want binaries", rec.Kind)
	}
	if rec.Dest != binDir {
		t.Errorf("Dest = %q, want %q", rec.Dest, binDir)
	}
	wantFiles := []string{filepath.Join(binDir, "zipts"), filepath.Join(binDir, "zipls")}
	if !slices.Equal(rec.Files, wantFiles) {
		t.Errorf("Files = %v, want %v", rec.Files, wantFiles)
	}
	if want := srv.URL + "/zipp-0.8.1.zip"; rec.Source != want {
		t.Errorf("Source = %q, want %q", rec.Source, want)
	}
	if want := sha256hex(zipData); rec.SHA256 != want {
		t.Errorf("SHA256 = %q, want the hash of the downloaded artifact %q", rec.SHA256, want)
	}
	if rec.InstalledAt.IsZero() {
		t.Error("InstalledAt was not set")
	}

	// The recorded paths must be the ones actually on disk: this is what makes
	// the record usable by uninstall.
	for _, p := range rec.Files {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("recorded file %s is not on disk: %v", p, err)
		}
	}
}

// TestPipelineDoesNotAnnounceSuccessBeforeStateSaveSucceeds is a regression
// test for AUDIT-OBSERVABILITY.md M3: "Installed ✓" must not be reported
// before the state record is actually saved. Before the fix, a failing state
// save (disk full, contended lock, ...) produced the contradictory sequence
// "✓ Installed rg ... → /path" immediately followed by "✗ save state: ...",
// while the tool was on disk but untracked by ls/upgrade/uninstall.
func TestPipelineDoesNotAnnounceSuccessBeforeStateSaveSucceeds(t *testing.T) {
	// Point XDG_STATE_HOME at a path that already exists as a regular file:
	// state.Update's directory creation then fails deterministically.
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blocker)

	zipData := makeFakeZip("tool-1.0.0", "bin/tool", []byte("payload"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {Backend: "url", Source: srv.URL + "/tool-{{version}}.zip", Archive: "zip", StripComponents: 1},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: dest},
		},
	}

	var oks []string
	hooks := &Hooks{OnOK: func(msg string) { oks = append(oks, msg) }}
	err := Run(context.Background(), cfg, "tool", nil, hooks)

	if err == nil {
		t.Fatal("Run succeeded despite an unwritable state directory")
	}
	if !strings.Contains(err.Error(), "save state") {
		t.Errorf("error = %v, want it to mention the state save failure", err)
	}
	for _, msg := range oks {
		if strings.Contains(msg, "Installed") {
			t.Errorf("OnOK reported %q before the state save that failed", msg)
		}
	}
}

// TestPipelineSkipsWhenAlreadyInstalled verifies that a second install of the
// same version does no work at all, and that Force overrides the skip. Without
// this, a broken skip check makes every `paq install` re-download and
// re-extract everything, which no other test would notice.
func TestPipelineSkipsWhenAlreadyInstalled(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("tool-1.0.0", "bin/tool", []byte("payload"))

	var downloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.Write(zipData)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend:         "url",
				Source:          srv.URL + "/tool-{{version}}.zip",
				Archive:         "zip",
				StripComponents: 1,
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: dest},
		},
	}

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("first install failed: %v", err)
	}
	if got := downloads.Load(); got != 1 {
		t.Fatalf("first install made %d downloads, want 1", got)
	}

	var messages []string
	hooks := &Hooks{OnOK: func(msg string) { messages = append(messages, msg) }}
	if err := Run(context.Background(), cfg, "tool", nil, hooks); err != nil {
		t.Fatalf("second install failed: %v", err)
	}
	if got := downloads.Load(); got != 1 {
		t.Errorf("second install made %d downloads in total, want 1: an already installed version must not be re-downloaded", got)
	}
	if len(messages) == 0 || !strings.Contains(messages[len(messages)-1], "already installed") {
		t.Errorf("messages = %v, want the last one to report the app as already installed", messages)
	}

	if err := Run(context.Background(), cfg, "tool", nil, &Hooks{Force: true}); err != nil {
		t.Fatalf("forced reinstall failed: %v", err)
	}
	if got := downloads.Load(); got != 2 {
		t.Errorf("forced reinstall made %d downloads in total, want 2: --force must bypass the skip", got)
	}
}

// TestExpandHomeFailsWithoutHome verifies that a ~ which cannot be expanded is
// an error rather than a path passed through unchanged: the unexpanded form
// would make the installer create a directory literally named "~".
func TestExpandHomeFailsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if got, err := expandHome("~/bin/rg"); err == nil {
		t.Fatalf("expandHome = %q, want an error when the home directory is unknown", got)
	}

	const abs = "/opt/tools/rg"
	if got, err := expandHome(abs); err != nil || got != abs {
		t.Errorf("expandHome(%q) = (%q, %v), want it returned unchanged", abs, got, err)
	}
}

func TestBuildAuxURL(t *testing.T) {
	cases := []struct {
		name        string
		downloadURL string
		assetName   string
		auxName     string
		want        string
		wantErr     bool
	}{
		{
			name:        "happy path",
			downloadURL: "https://example.com/dl/tool-1.0.0.tar.gz",
			assetName:   "tool-1.0.0.tar.gz",
			auxName:     "tool-1.0.0.tar.gz.sha256",
			want:        "https://example.com/dl/tool-1.0.0.tar.gz.sha256",
		},
		{
			name:        "url with query string",
			downloadURL: "https://example.com/dl/tool-1.0.0.tar.gz?token=abc",
			assetName:   "tool-1.0.0.tar.gz",
			auxName:     "tool-1.0.0.tar.gz.sha256",
			wantErr:     true,
		},
		{
			name:        "mismatched asset name",
			downloadURL: "https://example.com/dl/other.tar.gz",
			assetName:   "tool-1.0.0.tar.gz",
			auxName:     "tool-1.0.0.tar.gz.sha256",
			wantErr:     true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildAuxURL(c.downloadURL, c.assetName, c.auxName)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("buildAuxURL() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestFilesha256(t *testing.T) {
	if _, err := filesha256(filepath.Join(t.TempDir(), "nonexistent")); err == nil {
		t.Error("expected error for nonexistent path, got nil")
	}

	content := []byte("hello paq")
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, content, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := filesha256(f)
	if err != nil {
		t.Fatal(err)
	}
	if want := sha256hex(content); got != want {
		t.Errorf("filesha256() = %q, want %q", got, want)
	}
}

// TestPipelineSHA256URLAndJSON verifies a checksum published only by an API:
// sha256_url points at a document that is not a sibling of the asset, and
// sha256_json selects the hash inside it.
func TestPipelineSHA256URLAndJSON(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-tool-binary")
	tgzData := makeFakeTarGz(fileContent)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/checksums/linux":
			// The hash lives in a JSON API, under a path of its own: nothing
			// about this URL can be derived from the asset's.
			w.Write([]byte(`{"build":{"digest":"sha256:` + sha256hex(tgzData) + `"}}`))
		case "/dl/tool-1.0.0.tar.gz":
			w.Write(tgzData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend:         "url",
				Source:          srv.URL + "/dl/tool-{{version}}.tar.gz",
				Archive:         "tar.gz",
				StripComponents: 1,
				Verify: config.VerifyConfig{
					SHA256URL:  srv.URL + "/api/checksums/{{os}}",
					SHA256JSON: "build.digest",
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: dest},
		},
	}

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "rg"))
	if err != nil {
		t.Fatalf("installed file not found: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("installed content = %q, want %q", data, fileContent)
	}
}

// TestPipelineSHA256JSONMismatch verifies that a hash read from JSON is
// actually enforced: a wrong one must fail the install, not be ignored.
func TestPipelineSHA256JSONMismatch(t *testing.T) {
	isolateState(t)
	tgzData := makeFakeTarGz([]byte("fake-tool-binary"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			w.Write([]byte(`{"digest":"` + strings.Repeat("0", 64) + `"}`))
			return
		}
		w.Write(tgzData)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend: "url",
				Source:  srv.URL + "/tool-{{version}}.tar.gz",
				Archive: "tar.gz",
				Verify: config.VerifyConfig{
					SHA256URL:  srv.URL + "/checksums.json",
					SHA256JSON: "digest",
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: t.TempDir()},
		},
	}

	err := Run(context.Background(), cfg, "tool", nil, nil)
	if err == nil {
		t.Fatal("expected a checksum mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("error = %q, want a sha256 mismatch", err)
	}
}

// TestPipelineRejectsIncoherentSHA256Config verifies the two configurations
// that cannot mean anything are rejected before any network access.
func TestPipelineRejectsIncoherentSHA256Config(t *testing.T) {
	isolateState(t)
	cases := map[string]struct {
		verify config.VerifyConfig
		want   string
	}{
		"both sources": {
			verify: config.VerifyConfig{
				SHA256Asset: "{{asset}}.sha256",
				SHA256URL:   "https://unreachable.invalid/checksums.json",
			},
			want: "mutually exclusive",
		},
		"selector without a document": {
			verify: config.VerifyConfig{SHA256JSON: "digest"},
			want:   "sha256_json requires",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{
				Specs: map[string]config.Spec{
					"tool": {
						Backend: "url",
						Source:  "https://unreachable.invalid/tool-{{version}}.tar.gz",
						Archive: "tar.gz",
						Verify:  tc.verify,
					},
				},
				Apps: map[string]config.AppEntry{
					"tool": {Use: "tool", Version: "1.0.0", Dest: t.TempDir()},
				},
			}
			err := Run(context.Background(), cfg, "tool", nil, nil)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want mention of %q", err, tc.want)
			}
		})
	}
}
