package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/enr/paq/internal/state"
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

// State drift: the state DB says a tool is installed and its files are gone.
// paq cannot put this right on its own — ls, which, upgrade and uninstall all
// trust the record — so doctor must report it and fail.
func TestRunDoctorReportsStateDrift(t *testing.T) {
	doctorEnv(t, "")
	dir := t.TempDir()

	present := filepath.Join(dir, "bat")
	if err := os.WriteFile(present, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Set(state.InstalledApp{Name: "bat", Version: "0.24.0", Kind: "file", Dest: present})
	st.Set(state.InstalledApp{
		Name: "rg", Version: "14.1.1", Kind: "file",
		Dest: filepath.Join(dir, "rg-never-created"),
	})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	if err := runDoctor(doctorCmd, nil); err == nil {
		t.Fatal("runDoctor returned nil despite a record whose file is missing")
	}
}

// The counterpart: records that are all present must not trip the check.
func TestRunDoctorQuietWhenStateMatchesDisk(t *testing.T) {
	doctorEnv(t, "")
	dir := t.TempDir()

	present := filepath.Join(dir, "bat")
	if err := os.WriteFile(present, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Set(state.InstalledApp{Name: "bat", Version: "0.24.0", Kind: "file", Dest: present})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	if err := runDoctor(doctorCmd, nil); err != nil {
		t.Errorf("runDoctor: %v, want nil when every record is present", err)
	}
}

// paq doctor --json must be usable as a CI health gate: valid JSON on stdout
// (nothing else mixed in), a "problems" count matching the exit behavior, and
// at least one check flagged as the problem's source.
func TestRunDoctorJSON(t *testing.T) {
	doctorEnv(t, "this is not = valid toml [[[\n")

	var runErr error
	out := withJSON(t, func() {
		runErr = runDoctor(doctorCmd, nil)
	})

	if runErr == nil {
		t.Fatal("runDoctor returned nil for an unparsable manifest in --json mode")
	}

	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput:\n%s", err, out)
	}
	if report.Problems == 0 {
		t.Error("report.Problems = 0, want > 0 for an unparsable manifest")
	}

	var sawProblem bool
	for _, c := range report.Checks {
		if c.Name == "config" {
			if c.Status != "warn" {
				t.Errorf("config check status = %q, want %q", c.Status, "warn")
			}
			if !c.Problem {
				t.Error(`config check has Problem = false, want true for an unparsable manifest`)
			}
		}
		if c.Problem {
			sawProblem = true
		}
	}
	if !sawProblem {
		t.Error("no check in the report is marked as a problem, despite report.Problems > 0")
	}
}

// The healthy case must report zero problems and a clean exit, symmetric with
// TestRunDoctorSucceedsWhenNothingIsBroken.
func TestRunDoctorJSONSucceedsWhenNothingIsBroken(t *testing.T) {
	doctorEnv(t, "")

	var runErr error
	out := withJSON(t, func() {
		runErr = runDoctor(doctorCmd, nil)
	})
	if runErr != nil {
		t.Errorf("runDoctor: %v, want nil", runErr)
	}

	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput:\n%s", err, out)
	}
	if report.Problems != 0 {
		t.Errorf("report.Problems = %d, want 0", report.Problems)
	}
	if len(report.Checks) == 0 {
		t.Error("report.Checks is empty, want at least the platform/registry/... rows")
	}
}
