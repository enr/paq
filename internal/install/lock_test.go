package install

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/download"
	"github.com/enr/paq/internal/platform"
	"github.com/enr/paq/internal/verify"
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

	// The entry already pins this platform's archive: nothing to add to it.
	if err := config.WriteLockEntry("tool", config.LockEntry{Version: "1.0.0", Tag: "1.0.0", SHA256ByPlatform: map[string]string{platform.Detect().Key(): sha256hex(oldZip)}}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	lockPath, err := config.LockPath()
	if err != nil {
		t.Fatalf("LockPath: %v", err)
	}
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
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

	lockAfter, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	if string(lockAfter) != string(lockBefore) {
		t.Errorf("lockfile changed after reusing a locked version:\nbefore:\n%s\nafter:\n%s", lockBefore, lockAfter)
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
	if entry.Tag != "3.0.0" {
		t.Errorf("locked tag = %q, want %q (the raw resolved tag)", entry.Tag, "3.0.0")
	}
	want := map[string]string{platform.Detect().Key(): sha256hex(zipData)}
	if !reflect.DeepEqual(entry.SHA256ByPlatform, want) {
		t.Errorf("locked hashes = %v, want %v", entry.SHA256ByPlatform, want)
	}
}

// TestPipelineLockedTagKeepsBuildNumber covers a Temurin-style recipe whose
// asset name needs the build number, which only the tag carries: a lock
// entry must reinstall the same build without any live resolution.
func TestPipelineLockedTagKeepsBuildNumber(t *testing.T) {
	isolateState(t)
	isolateConfig(t)

	zipData := makeFakeZip("tool-21.0.12.1", "bin/tool", []byte("build-1"))
	var latestHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/latest.json"):
			latestHits.Add(1)
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/tool_21.0.12.1_1.zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := config.WriteLockEntry("tool", config.LockEntry{Version: "21.0.12.1", Tag: "jdk-21.0.12.1+1"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}

	spec := latestTrackingSpec(srv.URL+"/tool_{{version}}_{{version_build}}.zip", srv.URL+"/latest.json")
	spec.Tag = "jdk-{{version}}+{{version_build}}"
	dest := filepath.Join(t.TempDir(), "tool")
	cfg := &config.Config{
		Specs: map[string]config.Spec{"tool": spec},
		Apps:  map[string]config.AppEntry{"tool": {Use: "tool", Version: "latest", Dest: dest}},
		Lock:  lock,
	}

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if got := latestHits.Load(); got != 0 {
		t.Errorf("latest.json was hit %d time(s), want 0", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "tool")); err != nil {
		t.Errorf("installed file: %v", err)
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

// lockedToolConfig serves zipData as tool 1.0.0 and returns a config whose
// "latest"-tracking app is pinned to 1.0.0 by entry in paq.lock.toml.
func lockedToolConfig(t *testing.T, zipData []byte, entry config.LockEntry) (*config.Config, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tool-1.0.0.zip") {
			w.Write(zipData)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	if err := config.WriteLockEntry("tool", entry); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "tool")
	return &config.Config{
		Specs: map[string]config.Spec{
			"tool": latestTrackingSpec(srv.URL+"/tool-{{version}}.zip", srv.URL+"/latest.json"),
		},
		Apps: map[string]config.AppEntry{"tool": {Use: "tool", Version: "latest", Dest: dest}},
		Lock: lock,
	}, dest
}

// TestPipelineRejectsArchiveNotMatchingLock: the lock entry pins this
// platform's archive, so a different one served under the same release is a
// verification failure and nothing is installed.
func TestPipelineRejectsArchiveNotMatchingLock(t *testing.T) {
	isolateState(t)
	isolateConfig(t)

	served := makeFakeZip("tool-1.0.0", "bin/tool", []byte("tampered"))
	locked := makeFakeZip("tool-1.0.0", "bin/tool", []byte("original"))
	cfg, dest := lockedToolConfig(t, served, config.LockEntry{
		Version: "1.0.0", Tag: "1.0.0",
		SHA256ByPlatform: map[string]string{platform.Detect().Key(): sha256hex(locked)},
	})

	err := Run(context.Background(), cfg, "tool", nil, nil)
	if !errors.Is(err, verify.ErrVerification) || !strings.Contains(err.Error(), "paq.lock.toml") {
		t.Fatalf("Run() error = %v, want a verification failure naming paq.lock.toml", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("dest exists after a lock mismatch (stat err = %v), want nothing installed", err)
	}
}

// TestPipelineRecordsMissingPlatformHash: an entry written on another
// platform (or by a pre-0.4.0 paq, with no platform hashes) says nothing
// about this platform's archive; the install goes ahead and adds this
// platform's hash next to the others.
func TestPipelineRecordsMissingPlatformHash(t *testing.T) {
	isolateState(t)
	isolateConfig(t)

	zipData := makeFakeZip("tool-1.0.0", "bin/tool", []byte("payload"))
	cfg, dest := lockedToolConfig(t, zipData, config.LockEntry{
		Version: "1.0.0", Tag: "1.0.0",
		SHA256ByPlatform: map[string]string{"plan9-mips": sha256hex([]byte("another archive"))},
	})

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "tool")); err != nil {
		t.Errorf("installed file: %v", err)
	}

	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	want := config.LockEntry{Version: "1.0.0", Tag: "1.0.0", SHA256ByPlatform: map[string]string{
		"plan9-mips":            sha256hex([]byte("another archive")),
		platform.Detect().Key(): sha256hex(zipData),
	}}
	if got := lock.Apps["tool"]; !reflect.DeepEqual(got, want) {
		t.Errorf("lock entry = %+v, want %+v", got, want)
	}
}

// TestPipelineDownloadCacheKeyedByLock: a recipe with no checksum still gets
// cache hits through the lock entry's hash for this platform.
func TestPipelineDownloadCacheKeyedByLock(t *testing.T) {
	isolateState(t)
	isolateConfig(t)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	t.Setenv("PAQ_DOWNLOAD_CACHE", cacheDir)

	zipData := makeFakeZip("tool-1.0.0", "bin/tool", []byte("payload"))
	archive := filepath.Join(t.TempDir(), "tool-1.0.0.zip")
	if err := os.WriteFile(archive, zipData, 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if err := download.ToCache(cacheDir, archive, sha256hex(zipData)); err != nil {
		t.Fatalf("ToCache: %v", err)
	}
	// The server has nothing: only the cache can satisfy the install.
	cfg, dest := lockedToolConfig(t, nil, config.LockEntry{
		Version: "1.0.0", Tag: "1.0.0",
		SHA256ByPlatform: map[string]string{platform.Detect().Key(): sha256hex(zipData)},
	})
	cfg.Specs["tool"] = latestTrackingSpec("http://127.0.0.1:1/tool-{{version}}.zip", "http://127.0.0.1:1/latest.json")

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "tool")); err != nil {
		t.Errorf("installed file: %v", err)
	}
}

// TestPipelineUpgradesPre040LockEntry: an entry written by an older paq (no
// tag, no platform hash) still installs, and is recorded again with its tag
// and this platform's hash.
func TestPipelineUpgradesPre040LockEntry(t *testing.T) {
	isolateState(t)
	isolateConfig(t)

	zipData := makeFakeZip("tool-1.0.0", "bin/tool", []byte("payload"))
	cfg, _ := lockedToolConfig(t, zipData, config.LockEntry{Version: "1.0.0"})

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	want := config.LockEntry{Version: "1.0.0", Tag: "v1.0.0", SHA256ByPlatform: map[string]string{platform.Detect().Key(): sha256hex(zipData)}}
	if got := lock.Apps["tool"]; !reflect.DeepEqual(got, want) {
		t.Errorf("lock entry = %+v, want %+v", got, want)
	}
}
