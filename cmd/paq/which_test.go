package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enr/paq/internal/state"
)

// installedFile creates a real file and returns its path. `which` verifies
// that a recorded path is still on disk, so a fixture pointing at a path that
// was never created is, correctly, not installed.
func installedFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunWhichFileKind(t *testing.T) {
	dir := t.TempDir()
	withStateHome(t, dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	rg := installedFile(t, dir, "rg")
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: rg})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"rg"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})

	want := rg + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRunWhichBinariesKind(t *testing.T) {
	dir := t.TempDir()
	withStateHome(t, dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	zipts := installedFile(t, dir, "zipts")
	zipls := installedFile(t, dir, "zipls")
	st.Set(state.InstalledApp{
		Name: "zipp", Version: "0.8.1", Kind: "binaries",
		Files: []string{zipts, zipls},
	})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"zipp"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})

	want := zipts + "\n" + zipls + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRunWhichVersionDisambiguation(t *testing.T) {
	dir := t.TempDir()
	withStateHome(t, dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	rg13 := installedFile(t, dir, "rg-13")
	rg14 := installedFile(t, dir, "rg-14")
	st.Set(state.InstalledApp{Name: "rg", Version: "13.0.0", Kind: "file", Dest: rg13})
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: rg14})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	// No version: prints all installed versions.
	out := captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"rg"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})
	if out != rg13+"\n"+rg14+"\n" && out != rg14+"\n"+rg13+"\n" {
		t.Errorf("unexpected output for ambiguous app: %q", out)
	}

	// With version: prints only that one.
	out = captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"rg@14.1.1"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})
	if out != rg14+"\n" {
		t.Errorf("output = %q, want %q", out, rg14+"\n")
	}
}

func TestRunWhichNotInstalled(t *testing.T) {
	dir := t.TempDir()
	withStateHome(t, dir)

	if err := runWhich(whichCmd, []string{"nope"}); err == nil {
		t.Fatal("expected an error for a tool that isn't installed")
	}
}

func TestRunWhichUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	withStateHome(t, dir)

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

// The drift this guards: `which` feeds scripts, so a recorded path that no
// longer exists must not be printed with a success exit code — the failure
// would resurface later as a confusing "no such file" somewhere else.
func TestRunWhichFailsWhenFilesAreGone(t *testing.T) {
	dir := t.TempDir()
	withStateHome(t, dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	gone := installedFile(t, dir, "rg")
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: gone})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() { runErr = runWhich(whichCmd, []string{"rg"}) })

	if runErr == nil {
		t.Fatal("runWhich returned nil for a record whose file is gone")
	}
	if out != "" {
		t.Errorf("output = %q, want nothing printed for a missing path", out)
	}
}

// A version that is still on disk must keep working even when a sibling
// version's files have been removed.
func TestRunWhichSkipsOnlyTheMissingVersion(t *testing.T) {
	dir := t.TempDir()
	withStateHome(t, dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	rg13 := installedFile(t, dir, "rg-13")
	rg14 := installedFile(t, dir, "rg-14")
	st.Set(state.InstalledApp{Name: "rg", Version: "13.0.0", Kind: "file", Dest: rg13})
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: rg14})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if err := os.Remove(rg13); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runWhich(whichCmd, []string{"rg"}); err != nil {
			t.Fatalf("runWhich: %v", err)
		}
	})
	if out != rg14+"\n" {
		t.Errorf("output = %q, want only the surviving version %q", out, rg14+"\n")
	}
}
