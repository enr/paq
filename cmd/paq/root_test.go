package main

import (
	"testing"

	"github.com/enr/paq/internal/ui"
)

// withJSONFlag sets flagJSON for the duration of the test and restores both it
// and ui.Global afterwards. PersistentPreRunE writes ui.Global as a side
// effect, so restoring flagJSON alone leaks JSON mode into every later test of
// this package.
func withJSONFlag(t *testing.T, v bool) {
	t.Helper()
	savedFlag, savedUI := flagJSON, ui.Global
	t.Cleanup(func() { flagJSON, ui.Global = savedFlag, savedUI })
	flagJSON = v
}

func TestPersistentPreRunERejectsJSONOnUnsupportedCommands(t *testing.T) {
	withJSONFlag(t, true)

	if err := rootCmd.PersistentPreRunE(installCmd, nil); err == nil {
		t.Error("expected --json to be rejected on `paq install`, got nil error")
	}
	if err := rootCmd.PersistentPreRunE(doctorCmd, nil); err == nil {
		t.Error("expected --json to be rejected on `paq doctor`, got nil error")
	}
}

func TestPersistentPreRunEAllowsJSONOnSupportedCommands(t *testing.T) {
	withJSONFlag(t, true)

	if err := rootCmd.PersistentPreRunE(lsCmd, nil); err != nil {
		t.Errorf("expected --json to be allowed on `paq ls`, got: %v", err)
	}
	if err := rootCmd.PersistentPreRunE(registryShowCmd, nil); err != nil {
		t.Errorf("expected --json to be allowed on `paq registry show`, got: %v", err)
	}
}

func TestPersistentPreRunEAllowsNonJSONInvocations(t *testing.T) {
	withJSONFlag(t, false)

	if err := rootCmd.PersistentPreRunE(installCmd, nil); err != nil {
		t.Errorf("expected no error without --json, got: %v", err)
	}
}

// TestPersistentPreRunEAppliesUIConfig verifies that the flags are actually
// transferred to the global UI config: it is the only effect the hook has
// besides the --json rejection, and nothing else covered it.
func TestPersistentPreRunEAppliesUIConfig(t *testing.T) {
	withJSONFlag(t, true)
	savedQuiet, savedDebug := flagQuiet, flagDebug
	t.Cleanup(func() { flagQuiet, flagDebug = savedQuiet, savedDebug })
	flagQuiet, flagDebug = true, true

	if err := rootCmd.PersistentPreRunE(lsCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	if !ui.Global.JSON || !ui.Global.Quiet || !ui.Global.Debug {
		t.Errorf("ui.Global = %+v, want JSON, Quiet and Debug set", ui.Global)
	}
	if !ui.Global.Verbose {
		t.Error("--debug must imply verbose output")
	}
}
