package main

import (
	"path/filepath"
	"testing"

	"github.com/enr/paq/internal/config"
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
	if err := rootCmd.PersistentPreRunE(upgradeCmd, nil); err == nil {
		t.Error("expected --json to be rejected on `paq upgrade`, got nil error")
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
	// doctor is the diagnostic surface: it must be scriptable/CI-gateable like
	// the other read-only commands, not excluded from --json.
	if err := rootCmd.PersistentPreRunE(doctorCmd, nil); err != nil {
		t.Errorf("expected --json to be allowed on `paq doctor`, got: %v", err)
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

// TestApplyConfigPathOverride verifies --config and PAQ_CONFIG both set
// config.PathOverride (flag wins over env), and that neither being set clears
// a value left over from an earlier command in the same process.
func TestApplyConfigPathOverride(t *testing.T) {
	savedFlag, savedOverride := flagConfig, config.PathOverride
	t.Cleanup(func() { flagConfig, config.PathOverride = savedFlag, savedOverride })

	home := t.TempDir()
	t.Setenv("HOME", home)

	flagConfig = filepath.Join(home, "from-flag.toml")
	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	if config.PathOverride != flagConfig {
		t.Errorf("PathOverride = %q, want %q (from --config)", config.PathOverride, flagConfig)
	}

	flagConfig = ""
	t.Setenv("PAQ_CONFIG", "~/from-env.toml")
	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	want := filepath.Join(home, "from-env.toml")
	if config.PathOverride != want {
		t.Errorf("PathOverride = %q, want %q (from PAQ_CONFIG, ~ expanded)", config.PathOverride, want)
	}

	t.Setenv("PAQ_CONFIG", "")
	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	if config.PathOverride != "" {
		t.Errorf("PathOverride = %q, want empty once neither --config nor PAQ_CONFIG is set", config.PathOverride)
	}
}
