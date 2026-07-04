package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enr/paq/internal/state"
)

func TestConfirmYesNo(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"", false},
		{"maybe\n", false},
	}

	for _, tc := range cases {
		got := confirmYesNo(strings.NewReader(tc.input), "Continue?")
		if got != tc.want {
			t.Errorf("confirmYesNo(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// TestRunUninstallNonTTYSkipsPrompt verifies that under `go test` (stdout is
// not a terminal) runUninstall proceeds without blocking on a confirmation
// prompt, even with --yes unset.
func TestRunUninstallNonTTYSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	binPath := filepath.Join(t.TempDir(), "rg")
	if err := os.WriteFile(binPath, []byte("binary"), 0755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: binPath})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	flagUninstallYes = false
	flagUninstallDryRun = false
	t.Cleanup(func() {
		flagUninstallYes = false
		flagUninstallDryRun = false
	})

	if err := runUninstall(uninstallCmd, []string{"rg"}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, stat err = %v", binPath, err)
	}
}

// TestRunUninstallMultiApp verifies that uninstall accepts several app names
// in one invocation and removes all of them.
func TestRunUninstallMultiApp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	rgPath := filepath.Join(t.TempDir(), "rg")
	batPath := filepath.Join(t.TempDir(), "bat")
	for _, p := range []string{rgPath, batPath} {
		if err := os.WriteFile(p, []byte("binary"), 0755); err != nil {
			t.Fatalf("write fake binary: %v", err)
		}
	}

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: rgPath})
	st.Set(state.InstalledApp{Name: "bat", Version: "0.24.0", Kind: "file", Dest: batPath})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	flagUninstallYes = false
	flagUninstallDryRun = false
	t.Cleanup(func() {
		flagUninstallYes = false
		flagUninstallDryRun = false
	})

	if err := runUninstall(uninstallCmd, []string{"rg", "bat"}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	for _, p := range []string{rgPath, batPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err = %v", p, err)
		}
	}
}

// TestRunUninstallMultiAppFailsFastOnUnknownName verifies that when one of
// several requested apps isn't installed, nothing is removed at all — not
// even the apps that were found and would have resolved successfully.
func TestRunUninstallMultiAppFailsFastOnUnknownName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	rgPath := filepath.Join(t.TempDir(), "rg")
	if err := os.WriteFile(rgPath, []byte("binary"), 0755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: rgPath})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	flagUninstallYes = false
	flagUninstallDryRun = false
	t.Cleanup(func() {
		flagUninstallYes = false
		flagUninstallDryRun = false
	})

	err = runUninstall(uninstallCmd, []string{"rg", "not-installed-xyz"})
	if err == nil {
		t.Fatal("expected an error for the unknown second argument")
	}

	if _, statErr := os.Stat(rgPath); statErr != nil {
		t.Errorf("rg should not have been removed when a later argument is invalid, stat err = %v", statErr)
	}
}

// TestRemoveRecordFilesRefusesHomeDir verifies that a "dir" kind record whose
// Dest is the user's home directory is refused instead of being wiped out
// (e.g. a manifest typo like dest = "~").
func TestRemoveRecordFilesRefusesHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	marker := filepath.Join(home, "marker")
	if err := os.WriteFile(marker, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := state.InstalledApp{Name: "oops", Version: "1.0.0", Kind: "dir", Dest: home}
	err := removeRecordFiles(rec)
	if err == nil || !strings.Contains(err.Error(), "refusing to remove") {
		t.Fatalf("expected 'refusing to remove' error, got %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("home directory contents were removed: %v", err)
	}
}

func TestParseAppRef(t *testing.T) {
	cases := []struct {
		ref, name, version string
	}{
		{"rg", "rg", ""},
		{"rg@14.1.1", "rg", "14.1.1"},
		{"node18@20.11.0", "node18", "20.11.0"},
	}

	for _, tc := range cases {
		name, version := parseAppRef(tc.ref)
		if name != tc.name || version != tc.version {
			t.Errorf("parseAppRef(%q) = (%q, %q), want (%q, %q)", tc.ref, name, version, tc.name, tc.version)
		}
	}
}
