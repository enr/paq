package main

import (
	"testing"
	"time"

	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/updatecheck"
	"github.com/spf13/cobra"
)

func TestNewerVersionAvailable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.0.6", "0.0.7", true},
		{"v0.0.6", "0.0.7", true},
		{"0.0.7", "0.0.7", false},
		{"0.0.8", "0.0.7", false},
		{"0.0.6", "", false}, // no cached latest yet
	}
	for _, c := range cases {
		if got := newerVersionAvailable(c.current, c.latest); got != c.want {
			t.Errorf("newerVersionAvailable(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// openGate sets up an environment where the notice is allowed to run:
// isolated cache/config dirs, a release version, interactive stderr, no
// quiet/json, and no opt-out env.
func openGate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(updateCheckEnv, "")

	oldVersion := Version
	oldGlobal := ui.Global
	oldTTY := stderrIsTTY
	Version = "0.0.6"
	ui.Global = ui.Config{}
	stderrIsTTY = func() bool { return true }
	t.Cleanup(func() {
		Version = oldVersion
		ui.Global = oldGlobal
		stderrIsTTY = oldTTY
	})
}

func TestNotifyGateOpen(t *testing.T) {
	ls := &cobra.Command{Use: "ls"}

	t.Run("open", func(t *testing.T) {
		openGate(t)
		if !notifyGateOpen(ls) {
			t.Fatal("expected gate open")
		}
	})

	t.Run("dev build", func(t *testing.T) {
		openGate(t)
		Version = "dev"
		if notifyGateOpen(ls) {
			t.Fatal("dev build should skip")
		}
	})

	t.Run("quiet", func(t *testing.T) {
		openGate(t)
		ui.Global.Quiet = true
		if notifyGateOpen(ls) {
			t.Fatal("quiet should skip")
		}
	})

	t.Run("json", func(t *testing.T) {
		openGate(t)
		ui.Global.JSON = true
		if notifyGateOpen(ls) {
			t.Fatal("json should skip")
		}
	})

	t.Run("non-tty", func(t *testing.T) {
		openGate(t)
		stderrIsTTY = func() bool { return false }
		if notifyGateOpen(ls) {
			t.Fatal("non-tty should skip")
		}
	})

	t.Run("env opt-out", func(t *testing.T) {
		openGate(t)
		t.Setenv(updateCheckEnv, "1")
		if notifyGateOpen(ls) {
			t.Fatal("env opt-out should skip")
		}
	})

	t.Run("excluded command", func(t *testing.T) {
		openGate(t)
		if notifyGateOpen(&cobra.Command{Use: "self-update"}) {
			t.Fatal("self-update should skip")
		}
	})
}

func TestMaybeNotifyUpdateSpawnsWhenStale(t *testing.T) {
	openGate(t)
	spawns := 0
	old := spawnBackgroundCheck
	spawnBackgroundCheck = func() { spawns++ }
	t.Cleanup(func() { spawnBackgroundCheck = old })

	// Stale cache: last checked well over the interval ago.
	seed := updatecheck.State{LastChecked: time.Now().Add(-48 * time.Hour), LatestVersion: "0.0.6"}
	if err := seed.Save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	maybeNotifyUpdate(&cobra.Command{Use: "ls"})

	if spawns != 1 {
		t.Fatalf("expected exactly 1 spawn, got %d", spawns)
	}
	// LastChecked must have been bumped so a failing worker won't re-spawn.
	got, err := updatecheck.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if time.Since(got.LastChecked) >= updateCheckInterval {
		t.Fatal("expected LastChecked to be bumped to now")
	}
}

func TestMaybeNotifyUpdateNoSpawnWhenFresh(t *testing.T) {
	openGate(t)
	spawns := 0
	old := spawnBackgroundCheck
	spawnBackgroundCheck = func() { spawns++ }
	t.Cleanup(func() { spawnBackgroundCheck = old })

	seed := updatecheck.State{LastChecked: time.Now(), LatestVersion: "0.0.6"}
	if err := seed.Save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	maybeNotifyUpdate(&cobra.Command{Use: "ls"})

	if spawns != 0 {
		t.Fatalf("expected no spawn, got %d", spawns)
	}
}
