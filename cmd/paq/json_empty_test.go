package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/enr/paq/internal/ui"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = orig

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

// withJSON runs fn with ui.Global.JSON set to true, restoring the previous
// global UI config afterwards so other tests are not affected.
func withJSON(t *testing.T, fn func()) string {
	t.Helper()
	saved := ui.Global
	ui.Global = ui.Config{JSON: true}
	defer func() { ui.Global = saved }()

	return captureStdout(t, fn)
}

func TestRunLsJSONEmptyPrintsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	out := withJSON(t, func() {
		if err := runLs(lsCmd, nil); err != nil {
			t.Fatalf("runLs: %v", err)
		}
	})

	if strings.TrimSpace(out) != "[]" {
		t.Errorf("runLs --json with no packages = %q, want %q", strings.TrimSpace(out), "[]")
	}
}

func TestListDefinitionsJSONNoMatchPrintsEmptyArray(t *testing.T) {
	out := withJSON(t, func() {
		if err := listDefinitions("no-such-tool-xyz"); err != nil {
			t.Fatalf("listDefinitions: %v", err)
		}
	})

	if strings.TrimSpace(out) != "[]" {
		t.Errorf("listDefinitions --json with no match = %q, want %q", strings.TrimSpace(out), "[]")
	}
}
