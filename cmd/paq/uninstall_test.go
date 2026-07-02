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
