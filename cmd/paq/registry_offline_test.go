package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// corruptCache points the cache dir at a temp dir holding a snapshot directory
// with no meta.json, i.e. a corrupt external registry cache.
func corruptCache(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "paq", "registry")
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", tmp)
		dir = filepath.Join(tmp, "paq", "cache", "registry")
	} else {
		t.Setenv("XDG_CACHE_HOME", tmp)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

// TestOfflineDegradation verifies that a corrupt external registry cache never
// breaks read-only commands: they fall back to the embedded registry.
func TestOfflineDegradation(t *testing.T) {
	corruptCache(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig must not fail on a corrupt cache: %v", err)
	}
	if _, ok := cfg.Specs["ripgrep"]; !ok {
		t.Error("embedded ripgrep should be available despite the corrupt cache")
	}

	cmds := map[string]func() error{
		"registry list":   func() error { return runRegistryList(nil, nil) },
		"registry status": func() error { return runRegistryStatus(nil, nil) },
		"doctor":          func() error { return runDoctor(nil, nil) },
	}
	for name, run := range cmds {
		if err := run(); err != nil {
			t.Errorf("%s failed on corrupt cache: %v", name, err)
		}
	}
}
