package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
)

// lockEnv isolates the config dir and writes a lock entry for each name.
func lockEnv(t *testing.T, names ...string) {
	t.Helper()
	doctorEnv(t, "")
	for _, name := range names {
		if err := config.WriteLockEntry(name, config.LockEntry{Version: "1.0.0", Tag: "v1.0.0"}); err != nil {
			t.Fatalf("WriteLockEntry(%s): %v", name, err)
		}
	}
}

func lockedApps(t *testing.T) map[string]config.LockEntry {
	t.Helper()
	lock, err := config.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	return lock.Apps
}

func TestLockDeleteRemovesOnlyNamedEntries(t *testing.T) {
	lockEnv(t, "rg", "bat", "fd")

	if err := runLockDelete(lockDeleteCmd, []string{"rg", "fd"}); err != nil {
		t.Fatalf("runLockDelete: %v", err)
	}
	apps := lockedApps(t)
	if _, ok := apps["bat"]; !ok || len(apps) != 1 {
		t.Errorf("lock entries = %v, want only bat", apps)
	}
}

// TestLockDeleteReportsMissingEntries: a name with no entry (a typo, an app
// pinned to a fixed version) is an error naming it, but the existing entries
// asked for are still removed.
func TestLockDeleteReportsMissingEntries(t *testing.T) {
	lockEnv(t, "rg", "bat")

	err := runLockDelete(lockDeleteCmd, []string{"rg", "nope", "typo"})
	var he hintError
	if !errors.As(err, &he) || !strings.Contains(he.msg, "nope, typo") {
		t.Fatalf("runLockDelete error = %v, want a hintError naming nope, typo", err)
	}
	apps := lockedApps(t)
	if _, ok := apps["rg"]; ok {
		t.Error("rg still locked: the entries that exist must be removed anyway")
	}
	if _, ok := apps["bat"]; !ok {
		t.Error("bat was removed but not named")
	}
}

func TestLockDeleteWithoutLockfile(t *testing.T) {
	lockEnv(t)

	var he hintError
	if err := runLockDelete(lockDeleteCmd, []string{"rg"}); !errors.As(err, &he) {
		t.Fatalf("runLockDelete error = %v, want a hintError", err)
	}
}

func TestLockDeleteNeedsAnApp(t *testing.T) {
	if err := lockDeleteCmd.Args(lockDeleteCmd, nil); err == nil {
		t.Error("`paq lock delete` with no app: want an args error")
	}
}
