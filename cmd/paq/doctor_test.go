package main

import (
	"os"
	"path/filepath"
	"testing"
)

// doctorEnv points paq's config/state/cache at temp dirs and writes manifest
// (when non-empty) as the user manifest.
func doctorEnv(t *testing.T, manifest string) {
	t.Helper()
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	if manifest == "" {
		return
	}
	dir := filepath.Join(cfgHome, "paq")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
}

// doctor is the problem-detection surface: a manifest paq cannot parse must
// make it fail, not produce a report that reads as healthy. Before this was
// fixed the config row was a green tick (the check was only os.Stat), the
// install-dir rows were silently skipped, and the command exited 0.
func TestRunDoctorFailsOnUnparsableManifest(t *testing.T) {
	doctorEnv(t, "this is not = valid toml [[[\n")

	err := runDoctor(doctorCmd, nil)
	if err == nil {
		t.Fatal("runDoctor returned nil for an unparsable manifest; it must report a problem")
	}
	if got := exitCodeFor(err); got != exitError {
		t.Errorf("exitCodeFor = %d, want %d", got, exitError)
	}
}

// The counterpart: advisories must not fail the command. A missing manifest,
// no state file yet and a bin dir outside PATH are all normal for a fresh
// install, and doctor failing on them would make it useless as a health check.
func TestRunDoctorSucceedsWhenNothingIsBroken(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest string
	}{
		{"no manifest", ""},
		{"valid manifest", "[apps.rg]\nuse = \"ripgrep\"\nversion = \"latest\"\n"},
		{"empty manifest", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorEnv(t, tc.manifest)
			if err := runDoctor(doctorCmd, nil); err != nil {
				t.Errorf("runDoctor: %v, want nil", err)
			}
		})
	}
}
