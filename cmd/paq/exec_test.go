package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/enr/paq/internal/state"
)

// runExecArgs parses args the way cobra does (so ArgsLenAtDash is set) and
// runs `paq exec`.
func runExecArgs(t *testing.T, args ...string) error {
	t.Helper()
	execCmd.Flags().SetInterspersed(true)
	if err := execCmd.Flags().Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return runExec(execCmd, execCmd.Flags().Args())
}

// execFixture records an installed file tool "tool" and returns its bin dir.
func execFixture(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	t.Setenv("PATH", os.Getenv("PATH")) // restored after runExec rewrites it
	withStateHome(t, t.TempDir())
	bin := t.TempDir()
	dest := filepath.Join(bin, "tool")
	if err := os.WriteFile(dest, []byte("#!/bin/sh\necho tool ran\n"), 0755); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Set(state.InstalledApp{Name: "tool", Version: "1.0.0", Kind: "file", Dest: dest})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestRunExecPutsToolFirstOnPATH(t *testing.T) {
	execFixture(t)
	var err error
	out := captureStdout(t, func() { err = runExecArgs(t, "tool", "--", "tool") })
	if err != nil {
		t.Fatalf("runExec: %v", err)
	}
	if strings.TrimSpace(out) != "tool ran" {
		t.Errorf("stdout = %q, want the tool's output", out)
	}
}

// paq exits with the command's own exit code, and reports nothing itself.
func TestRunExecPropagatesExitCode(t *testing.T) {
	execFixture(t)
	err := runExecArgs(t, "tool", "--", "sh", "-c", "exit 3")
	if got := exitCodeFor(err); got != 3 {
		t.Errorf("exit code = %d (err %v), want 3", got, err)
	}
}

// A command killed by a signal exits with 128 + the signal, as in a shell.
func TestRunExecSignalExitCode(t *testing.T) {
	execFixture(t)
	err := runExecArgs(t, "tool", "--", "sh", "-c", "kill -TERM $$")
	if got := exitCodeFor(err); got != 128+15 {
		t.Errorf("exit code = %d (err %v), want 143", got, err)
	}
}

func TestRunExecErrors(t *testing.T) {
	execFixture(t)
	if err := runExecArgs(t, "tool"); exitCodeFor(err) != exitUsage {
		t.Errorf("exec without `--`: err = %v, want a usage error", err)
	}
	if err := runExecArgs(t, "tool", "--"); exitCodeFor(err) != exitUsage {
		t.Errorf("exec without a command: err = %v, want a usage error", err)
	}
	err := runExecArgs(t, "other", "--", "true")
	if _, hint := messageAndHint(err); !strings.Contains(hint, "paq install other") {
		t.Errorf("exec of a tool not installed: err = %v, hint = %q, want an install hint", err, hint)
	}
	if err := runExecArgs(t, "tool@9.9.9", "--", "true"); err == nil {
		t.Error("exec of a version not installed: want an error")
	}
}

// Next to a project manifest, a tool missing in global mode suggests --local.
func TestLocalModeHint(t *testing.T) {
	withFlag(t, &flagLocal, false)
	t.Setenv("PAQ_LOCAL", "")
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	if hint := localModeHint(); hint != "" {
		t.Errorf("hint without a project manifest = %q, want none", hint)
	}
	if err := os.WriteFile(filepath.Join(root, localManifestName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if hint := localModeHint(); !strings.Contains(hint, "--local") || !strings.Contains(hint, localManifestName) {
		t.Errorf("hint = %q, want a --local suggestion naming the manifest", hint)
	}
	withFlag(t, &flagLocal, true)
	if hint := localModeHint(); hint != "" {
		t.Errorf("hint in local mode = %q, want none", hint)
	}
}
