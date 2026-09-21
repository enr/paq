package main

import (
	"io"
	"os"
	"runtime"
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
	// Restore even if fn calls t.Fatal, which unwinds via runtime.Goexit.
	defer func() { os.Stdout = orig }()

	// Drain concurrently so a capture larger than the pipe buffer cannot
	// deadlock fn's write.
	done := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()

	fn()

	w.Close()
	return string(<-done)
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

// withFlag sets *p to v for the duration of the test, restoring whatever
// value *p held before — not a hardcoded default — once the test ends.
func withFlag[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	t.Cleanup(func() { *p = old })
	*p = v
}

// withConfigHome points userConfigPath at dir, on Linux/macOS (XDG_CONFIG_HOME)
// and Windows (APPDATA) alike.
func withConfigHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
	}
}

// withStateHome points state.StatePath at dir, on Linux/macOS (XDG_STATE_HOME)
// and Windows (LOCALAPPDATA) alike.
func withStateHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", dir)
	}
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
	// listDefinitions goes through loadConfig: without this the user's real
	// manifest and registry cache decide the result.
	doctorEnv(t, "")

	out := withJSON(t, func() {
		if err := listDefinitions("no-such-tool-xyz"); err != nil {
			t.Fatalf("listDefinitions: %v", err)
		}
	})

	if strings.TrimSpace(out) != "[]" {
		t.Errorf("listDefinitions --json with no match = %q, want %q", strings.TrimSpace(out), "[]")
	}
}
