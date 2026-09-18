package install

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/enr/paq/internal/config"
)

// isolateConfig points paq's manifest (and therefore paq.lock.toml, which
// always lives next to it) at a fresh temp file for the duration of the test.
func isolateConfig(t *testing.T) {
	t.Helper()
	saved := config.PathOverride
	t.Cleanup(func() { config.PathOverride = saved })
	config.PathOverride = filepath.Join(t.TempDir(), "config.toml")
}

// latestTrackingSpec builds a "url"-backend spec whose "latest" resolves via
// the "json" strategy against srv, and counts how many times that endpoint is
// hit — the signal used below to prove whether a live resolution happened.
func latestTrackingSpec(assetURLTemplate, latestURL string) config.Spec {
	return config.Spec{
		Backend:         "url",
		Source:          assetURLTemplate,
		Archive:         "zip",
		StripComponents: 1,
		LatestStrategy:  "json",
		LatestURL:       latestURL,
		LatestJSON:      "version",
	}
}

func TestPipelineUsesLockedVersionInsteadOfLatest(t *testing.T) {
	isolateState(t)
	isolateConfig(t)

	oldZip := makeFakeZip("tool-1.0.0", "bin/tool", []byte("old"))
	newZip := makeFakeZip("tool-2.0.0", "bin/tool", []byte("new"))

	var latestHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/latest.json"):
			latestHits.Add(1)
			w.Write([]byte(`{"version":"2.0.0"}`))
		case strings.HasSuffix(r.URL.Path, "1.0.0.zip"):
			w.Write(oldZip)
		case strings.HasSuffix(r.URL.Path, "2.0.0.zip"):
			w.Write(newZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := config.WriteLockEntry("tool", config.LockEntry{Version: "1.0.0"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": latestTrackingSpec(srv.URL+"/tool-{{version}}.zip", srv.URL+"/latest.json"),
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "latest", Dest: dest},
		},
	}
	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	cfg.Lock = lock

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	if got := latestHits.Load(); got != 0 {
		t.Errorf("latest.json was hit %d time(s), want 0: a locked version must skip live resolution", got)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatalf("installed file: %v", err)
	}
	if string(data) != "old" {
		t.Errorf("installed content = %q, want %q (the locked version 1.0.0)", data, "old")
	}
}

func TestPipelineIgnoreLockForcesLiveResolutionAndRefreshesLock(t *testing.T) {
	isolateState(t)
	isolateConfig(t)

	newZip := makeFakeZip("tool-2.0.0", "bin/tool", []byte("new"))

	var latestHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/latest.json"):
			latestHits.Add(1)
			w.Write([]byte(`{"version":"2.0.0"}`))
		case strings.HasSuffix(r.URL.Path, "2.0.0.zip"):
			w.Write(newZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := config.WriteLockEntry("tool", config.LockEntry{Version: "1.0.0"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": latestTrackingSpec(srv.URL+"/tool-{{version}}.zip", srv.URL+"/latest.json"),
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "latest", Dest: dest},
		},
	}
	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	cfg.Lock = lock

	// Mirrors what `paq upgrade` sets: resolve and install the newest release
	// even though an older one is pinned in the lockfile.
	if err := Run(context.Background(), cfg, "tool", nil, &Hooks{IgnoreLock: true}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	if got := latestHits.Load(); got != 1 {
		t.Errorf("latest.json was hit %d time(s), want 1: IgnoreLock must force live resolution", got)
	}

	newLock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if got := newLock.Apps["tool"].Version; got != "2.0.0" {
		t.Errorf("lock entry = %q, want %q (refreshed to the newly resolved version)", got, "2.0.0")
	}
}

func TestPipelineLocksNewlyResolvedLatestVersion(t *testing.T) {
	isolateState(t)
	isolateConfig(t)

	zipData := makeFakeZip("tool-3.0.0", "bin/tool", []byte("payload"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/latest.json"):
			w.Write([]byte(`{"version":"3.0.0"}`))
		case strings.HasSuffix(r.URL.Path, "3.0.0.zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": latestTrackingSpec(srv.URL+"/tool-{{version}}.zip", srv.URL+"/latest.json"),
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "latest", Dest: dest},
		},
	}
	// No pre-existing lock entry: cfg.Lock is nil, exactly as a caller that
	// never touches the lockfile would leave it (pipeline must nil-check it).

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	entry, ok := lock.Apps["tool"]
	if !ok {
		t.Fatal("no lock entry written for a freshly resolved \"latest\" install")
	}
	if entry.Version != "3.0.0" {
		t.Errorf("locked version = %q, want %q", entry.Version, "3.0.0")
	}
	if entry.SHA256 != sha256hex(zipData) {
		t.Errorf("locked sha256 = %q, want the artifact's hash %q", entry.SHA256, sha256hex(zipData))
	}
}

func TestPipelineFixedVersionDoesNotWriteLock(t *testing.T) {
	isolateState(t)
	isolateConfig(t)

	zipData := makeFakeZip("tool-1.0.0", "bin/tool", []byte("payload"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {Backend: "url", Source: srv.URL + "/tool-{{version}}.zip", Archive: "zip"},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: dest}, // pinned, not "latest"
		},
	}

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if _, ok := lock.Apps["tool"]; ok {
		t.Error("a pinned version must not get a lock entry: it's already deterministic")
	}
}
