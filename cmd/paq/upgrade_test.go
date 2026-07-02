package main

import (
	"context"
	"testing"

	"github.com/enr/paq/internal/config"
)

// TestRunUpgradeMultiArgFailsFastOnUnknownName verifies that when multiple
// app names are given, an unknown one makes the whole command fail before
// any upgrade is attempted, regardless of its position in the argument list.
func TestRunUpgradeMultiArgFailsFastOnUnknownName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	block := renderAppEntryTOML("rg", config.AppEntry{Use: "ripgrep", Version: "14.1.1"})
	if _, err := config.WriteManifestEntry("rg", block, false); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}

	if err := runUpgrade(upgradeCmd, []string{"rg", "typo-xyz-does-not-exist"}); err == nil {
		t.Error("expected an error when the second argument is unknown")
	}
	if err := runUpgrade(upgradeCmd, []string{"typo-xyz-does-not-exist", "rg"}); err == nil {
		t.Error("expected an error when the first argument is unknown")
	}
}

// TestRunUpgradeMultiArgPinnedSkipsWithoutError verifies that upgrade accepts
// several app names pinned to a fixed version: each is skipped (no network
// call, since they're not "latest") and the command succeeds.
func TestRunUpgradeMultiArgPinnedSkipsWithoutError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for name, use := range map[string]string{"rg": "ripgrep", "bat": "bat"} {
		block := renderAppEntryTOML(name, config.AppEntry{Use: use, Version: "1.0.0"})
		if _, err := config.WriteManifestEntry(name, block, false); err != nil {
			t.Fatalf("write manifest entry for %s: %v", name, err)
		}
	}

	// cobra only sets Command.Context() during Execute(); set it explicitly
	// since this test calls runUpgrade directly and it reaches the
	// errgroup.WithContext call for these (valid, pinned) apps.
	upgradeCmd.SetContext(context.Background())
	t.Cleanup(func() { upgradeCmd.SetContext(nil) })

	if err := runUpgrade(upgradeCmd, []string{"rg", "bat"}); err != nil {
		t.Errorf("expected pinned apps to be skipped without error, got: %v", err)
	}
}
