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
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
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
				Backend: "github",
				Repo:    "test/ripgrep",
				Asset:   "ripgrep-{{version}}-x86_64-unknown-linux-gnu.tar.gz",
				Archive: "tar.gz",
				Extract: "rg",
				Chmod:   "0755",
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
				Backend: "github",
				Repo:    "test/ripgrep",
				Asset:   "ripgrep-{{version}}-x86_64-unknown-linux-gnu.tar.gz",
				Archive: "tar.gz",
				Extract: "rg",
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
