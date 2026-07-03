package main

import (
	"os"
	"time"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/updatecheck"
	"github.com/enr/paq/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// updateCheckInterval is the minimum time between background release lookups.
const updateCheckInterval = 24 * time.Hour

// updateCheckEnv disables the daily update check when set to any value.
const updateCheckEnv = "PAQ_NO_UPDATE_CHECK"

// notifyExcludedCommands are the commands that never trigger the update
// notice: self-update does its own check, version/completion must emit clean
// output, help is not a real run, and __update-check is the background worker
// itself (avoiding recursion).
var notifyExcludedCommands = map[string]bool{
	"self-update":    true,
	"version":        true,
	"completion":     true,
	"help":           true,
	"__update-check": true,
}

// spawnBackgroundCheck launches the detached "paq __update-check" worker.
// It is a package var so tests can stub the side effect.
var spawnBackgroundCheck = spawnDetached

// stderrIsTTY reports whether stderr is an interactive terminal. The notice is
// printed to stderr, so scripts and CI redirecting or capturing it stay clean.
// It is a package var so tests can stub the terminal check.
var stderrIsTTY = func() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// maybeNotifyUpdate prints a one-line hint when a newer paq release is known,
// and refreshes the cached release info in the background once a day. It never
// blocks on the network and never returns an error to the caller.
func maybeNotifyUpdate(cmd *cobra.Command) {
	if !notifyGateOpen(cmd) {
		return
	}

	st, err := updatecheck.Load()
	if err != nil {
		return
	}

	if newerVersionAvailable(Version, st.LatestVersion) {
		ui.Hint("A new version of paq is available: %s → %s. Run 'paq self-update'.", Version, st.LatestVersion)
	}

	if time.Since(st.LastChecked) >= updateCheckInterval {
		// Bump the timestamp before spawning so a failing worker cannot make
		// every subsequent command re-spawn within the interval.
		st.LastChecked = time.Now()
		if err := st.Save(); err != nil {
			return
		}
		spawnBackgroundCheck()
	}
}

// notifyGateOpen reports whether the update notice may run for this command:
// enabled, a release build, interactive, not quiet/json, not an excluded command.
func notifyGateOpen(cmd *cobra.Command) bool {
	if updateCheckDisabled() || Version == "dev" ||
		ui.Global.Quiet || ui.Global.JSON ||
		!stderrIsTTY() || notifyExcludedCommands[cmd.Name()] {
		return false
	}
	return true
}

// newerVersionAvailable reports whether latest is a known version strictly
// newer than the running one.
func newerVersionAvailable(current, latest string) bool {
	return latest != "" && version.Compare(version.Clean(current), latest) < 0
}

// updateCheckDisabled reports whether the daily check is turned off via the
// PAQ_NO_UPDATE_CHECK env var or `[defaults] check_updates = false`.
func updateCheckDisabled() bool {
	if os.Getenv(updateCheckEnv) != "" {
		return true
	}
	cfg, err := config.LoadUserConfig()
	if err != nil {
		return false
	}
	return cfg.Defaults.CheckUpdates != nil && !*cfg.Defaults.CheckUpdates
}
