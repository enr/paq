package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enr/paq/internal/state"
)

// lsState seeds the state DB with one present and one missing record and
// returns the path of the present one.
func lsState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	withStateHome(t, dir)

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
	return present
}

// The table keeps its shape — a missing tool is reported beside it, not by
// adding a column — so the rows themselves must be unchanged.
func TestRunLsKeepsTableShapeWithDrift(t *testing.T) {
	present := lsState(t)

	out := captureStdout(t, func() {
		if err := runLs(lsCmd, nil); err != nil {
			t.Fatalf("runLs: %v", err)
		}
	})

	if !strings.Contains(out, "NAME") || !strings.Contains(out, "DEST") {
		t.Errorf("expected the usual header, got:\n%s", out)
	}
	if strings.Contains(out, "MISSING") || strings.Contains(out, "STATUS") {
		t.Errorf("table format should be unchanged, got:\n%s", out)
	}
	for _, want := range []string{present, "rg-never-created"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the table, got:\n%s", want, out)
		}
	}
}

// --json carries the fact per entry, so scripts can see the drift the warning
// line conveys to humans.
func TestRunLsJSONMarksMissingEntries(t *testing.T) {
	lsState(t)

	out := withJSON(t, func() {
		if err := runLs(lsCmd, nil); err != nil {
			t.Fatalf("runLs: %v", err)
		}
	})

	var entries []struct {
		Name    string `json:"name"`
		Missing bool   `json:"missing"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("ls --json is not valid JSON: %v\n%s", err, out)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	for _, e := range entries {
		want := e.Name == "rg"
		if e.Missing != want {
			t.Errorf("%s: missing = %v, want %v", e.Name, e.Missing, want)
		}
	}
}

// The "missing" field is a fact about the filesystem, not something to
// persist: it must never leak into the state file's schema.
func TestStateFileHasNoMissingField(t *testing.T) {
	lsState(t)

	path, err := state.StatePath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "missing") {
		t.Errorf("state.json must not carry a \"missing\" field:\n%s", data)
	}
}
