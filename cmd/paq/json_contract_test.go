package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
)

// With --json, a failing command prints one error document on stdout, with
// the exit code and its stable name, so a script never parses stderr.
func TestReportErrorPrintsJSONDocument(t *testing.T) {
	out := withJSON(t, func() {
		reportError(hintError{msg: "boom", hint: "do this"})
	})

	var doc jsonErrorDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n%s", err, out)
	}
	want := jsonErrorBody{Code: "error", ExitCode: exitError, Message: "boom", Hint: "do this"}
	if doc.Error != want {
		t.Errorf("error doc = %+v, want %+v", doc.Error, want)
	}
}

func TestReportErrorJSONUsageCode(t *testing.T) {
	out := withJSON(t, func() {
		reportError(usageError{"--dest needs exactly one app"})
	})

	var doc jsonErrorDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n%s", err, out)
	}
	if doc.Error.Code != "usage" || doc.Error.ExitCode != exitUsage {
		t.Errorf("error doc = %+v, want code usage / exit %d", doc.Error, exitUsage)
	}
}

// A command that already printed its result (doctor's report, audit's
// findings, install's per-app outcomes) keeps stdout to that one document.
func TestReportErrorSkipsJSONWhenResultPrinted(t *testing.T) {
	out := withJSON(t, func() {
		ui.JSONWritten = true
		reportError(errors.New("1 problem(s) found"))
	})
	if out != "" {
		t.Errorf("stdout = %q, want nothing after the command's own JSON", out)
	}
}

// runParallel reports every app's outcome in --json mode, sorted by name,
// failures included with their error and hint.
func TestRunParallelPrintsResultsJSON(t *testing.T) {
	var runErr error
	out := withJSON(t, func() {
		runErr = runParallel(context.Background(), []string{"zeta", "alpha"}, "installed",
			func(ctx context.Context, name string, hooks *install.Hooks, res *ui.ActionResult) error {
				if name == "zeta" {
					return hintError{msg: "zeta broke", hint: "fix zeta"}
				}
				res.Status, res.Version, res.Dest = "installed", "1.0.0", "/bin/alpha"
				return nil
			})
	})
	if runErr == nil {
		t.Fatal("expected an error for the failed app")
	}

	var got []ui.ActionResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, out)
	}
	want := []ui.ActionResult{
		{Name: "alpha", Status: "installed", Version: "1.0.0", Dest: "/bin/alpha"},
		{Name: "zeta", Status: "failed", Error: "zeta broke", Hint: "fix zeta"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("results = %+v, want %+v", got, want)
	}
}

func TestRunUninstallJSON(t *testing.T) {
	withStateHome(t, t.TempDir())
	rgPath := filepath.Join(t.TempDir(), "rg")
	if err := os.WriteFile(rgPath, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1", Kind: "file", Dest: rgPath})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	withFlag(t, &flagUninstallDryRun, false)

	// --json owns stdout, so there is nobody to prompt: --yes is required,
	// and its absence is a usage error.
	withFlag(t, &flagUninstallYes, false)
	withFlag(t, &uninstallIsTTY, func() bool { return true })
	withJSON(t, func() { err = runUninstall(uninstallCmd, []string{"rg"}) })
	if exitCodeFor(err) != exitUsage {
		t.Fatalf("runUninstall --json without --yes: err = %v, want a usage error", err)
	}

	withFlag(t, &flagUninstallYes, true)
	out := withJSON(t, func() { err = runUninstall(uninstallCmd, []string{"rg"}) })
	if err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	var got []ui.ActionResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, out)
	}
	want := ui.ActionResult{Name: "rg", Status: "uninstalled", Version: "14.1.1", Dest: rgPath}
	if len(got) != 1 || got[0] != want {
		t.Errorf("results = %+v, want [%+v]", got, want)
	}
}

// --strict turns doctor's warnings (here: no manifest, no state yet) into a
// failure, for CI gates; without it the same setup passes.
func TestRunDoctorStrict(t *testing.T) {
	doctorEnv(t, "")
	withFlag(t, &doctorStrict, true)

	var runErr error
	out := withJSON(t, func() { runErr = runDoctor(doctorCmd, nil) })
	if runErr == nil || !strings.Contains(runErr.Error(), "--strict") {
		t.Fatalf("runDoctor --strict = %v, want a warnings error", runErr)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if report.Warnings == 0 || report.Problems != 0 {
		t.Errorf("report problems=%d warnings=%d, want 0 problems and some warnings", report.Problems, report.Warnings)
	}
}
