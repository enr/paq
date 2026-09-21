package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// Isolate config and state first: loadConfig reads the user manifest, and
	// an unparsable one on the developer's machine would fail this test for a
	// reason that has nothing to do with the corrupt cache under test.
	doctorEnv(t, "")
	corruptCache(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig must not fail on a corrupt cache: %v", err)
	}
	if _, ok := cfg.Specs["ripgrep"]; !ok {
		t.Error("embedded ripgrep should be available despite the corrupt cache")
	}

	listOut := captureStdout(t, func() {
		if err := runRegistryList(nil, nil); err != nil {
			t.Fatalf("registry list failed on corrupt cache: %v", err)
		}
	})
	if !strings.Contains(listOut, "ripgrep") {
		t.Errorf("registry list fell back to an empty registry:\n%s", listOut)
	}

	statusOut := captureStdout(t, func() {
		if err := runRegistryStatus(nil, nil); err != nil {
			t.Fatalf("registry status failed on corrupt cache: %v", err)
		}
	})
	if strings.Contains(statusOut, "Active recipes: 0") {
		t.Errorf("registry status reports zero active recipes despite the embedded fallback:\n%s", statusOut)
	}

	var runErr error
	doctorOut := withJSON(t, func() {
		runErr = runDoctor(nil, nil)
	})
	if runErr != nil {
		t.Fatalf("doctor failed on corrupt cache: %v", runErr)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(doctorOut), &report); err != nil {
		t.Fatalf("doctor stdout is not valid JSON: %v\noutput:\n%s", err, doctorOut)
	}
	found := false
	for _, c := range report.Checks {
		if c.Name == "registry" {
			found = true
			if c.Problem {
				t.Errorf("registry check is a problem despite the embedded fallback: %+v", c)
			}
		}
	}
	if !found {
		t.Errorf("doctor report is missing the registry check: %+v", report.Checks)
	}
}
