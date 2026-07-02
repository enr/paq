package main

import (
	"testing"

	"github.com/enr/paq/internal/state"
)

func TestRunWhichFileKind(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: "/home/user/.local/bin/rg"})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"rg"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})

	want := "/home/user/.local/bin/rg\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRunWhichBinariesKind(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	st.Set(state.InstalledApp{
		Name: "zipp", Version: "0.8.1", Kind: "binaries",
		Files: []string{"/home/user/.local/bin/zipts", "/home/user/.local/bin/zipls"},
	})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"zipp"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})

	want := "/home/user/.local/bin/zipts\n/home/user/.local/bin/zipls\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRunWhichVersionDisambiguation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	st.Set(state.InstalledApp{Name: "rg", Version: "13.0.0", Kind: "file", Dest: "/opt/rg-13"})
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: "/opt/rg-14"})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	// No version: prints all installed versions.
	out := captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"rg"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})
	if out != "/opt/rg-13\n/opt/rg-14\n" && out != "/opt/rg-14\n/opt/rg-13\n" {
		t.Errorf("unexpected output for ambiguous app: %q", out)
	}

	// With version: prints only that one.
	out = captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"rg@14.1.1"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})
	if out != "/opt/rg-14\n" {
		t.Errorf("output = %q, want %q", out, "/opt/rg-14\n")
	}
}

func TestRunWhichNotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	if err := runWhich(whichCmd, []string{"nope"}); err == nil {
		t.Fatal("expected an error for a tool that isn't installed")
	}
}

func TestRunWhichUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: "/opt/rg"})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if err := runWhich(whichCmd, []string{"rg@99.0.0"}); err == nil {
		t.Fatal("expected an error for a version that isn't installed")
	}
}
