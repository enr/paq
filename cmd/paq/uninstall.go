package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var (
	flagUninstallDryRun bool
	flagUninstallYes    bool
)

// uninstallIsTTY reports whether stdout is an interactive terminal. It is a
// package var so tests can stub the terminal check (mirrors stderrIsTTY in
// update_notify.go).
var uninstallIsTTY = ui.IsTTY

var uninstallCmd = &cobra.Command{
	Use:     "uninstall <app[@version]>...",
	Aliases: []string{"rm", "remove"},
	Short:   "Uninstall one or more tools (use app@version to disambiguate multiple versions)",
	Example: `  paq uninstall rg
  paq uninstall rg bat      # uninstall multiple tools
  paq uninstall rg@14.0.0   # disambiguate when multiple versions are installed
  paq uninstall rg --dry-run`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeInstalledApps,
	RunE:              runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVar(&flagUninstallDryRun, "dry-run", false, "Show what would be removed without removing anything")
	uninstallCmd.Flags().BoolVarP(&flagUninstallYes, "yes", "y", false, "Skip confirmation prompt (required in non-interactive sessions)")
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	// Resolve every argument to its target state entries before removing
	// anything or prompting, so an invalid name later in the list fails
	// without touching apps that came before it.
	var targets []state.InstalledApp
	var names []string
	for _, arg := range args {
		appTargets, err := resolveUninstallTargets(st, arg)
		if err != nil {
			return err
		}
		targets = append(targets, appTargets...)
		names = append(names, appTargets[0].Name)
	}

	if flagUninstallDryRun {
		printUninstallTargets("dry-run: would remove:", targets)
		return nil
	}

	// Ask for confirmation unless --yes was passed. A non-interactive session
	// (no TTY on stdout) can't be prompted, so it must pass --yes explicitly
	// instead of proceeding unconfirmed.
	if !flagUninstallYes {
		if !uninstallIsTTY() {
			return fmt.Errorf("refusing to uninstall without confirmation in a non-interactive session: pass --yes")
		}
		printUninstallTargets("This will remove:", targets)
		if !confirmYesNo(os.Stdin, "Continue?") {
			ui.Step("aborted")
			return nil
		}
	}

	// Perform the removal under the cross-process state lock (state.Update):
	// the confirmation prompt above can block for an arbitrary time, so the
	// state loaded at the top may be stale by now. Re-reading it inside the
	// lock keeps a stale save from silently dropping records written by a
	// concurrent paq process in the meantime.
	//
	// Paths still owned by records that survive this uninstall must not be
	// deleted, even if a target record points at them too. This guards the
	// case where two records share a destination (e.g. two versions of a dir
	// app left pointing at the same folder): removing one must not wipe the
	// files the other still owns.
	if err := state.Update(func(st *state.State) error {
		keep := survivingPaths(st, targets)
		for _, rec := range targets {
			ui.Step("Uninstalling %s %s from %s...", rec.Name, rec.Version, rec.Dest)
			if err := removeRecordFiles(rec, keep); err != nil {
				return err
			}
			st.Delete(rec.Name, rec.Version)
		}
		return nil
	}); err != nil {
		return err
	}

	ui.OK("%s uninstalled", strings.Join(names, ", "))
	return nil
}

// resolveUninstallTargets resolves a single "name" or "name@version" argument
// to the state entries it refers to, without removing anything. Returns a
// hintError if the app isn't installed, the requested version isn't
// installed, or the app has multiple versions installed and none was
// specified (ambiguous).
func resolveUninstallTargets(st *state.State, arg string) ([]state.InstalledApp, error) {
	name, version := parseAppRef(arg)

	matches := st.ByName(name)
	if len(matches) == 0 {
		return nil, hintError{
			msg:  fmt.Sprintf("%q is not installed", name),
			hint: "list installed tools with `paq ls`",
		}
	}

	if version != "" {
		rec, ok := st.Get(name, version)
		if !ok {
			return nil, hintError{
				msg:  fmt.Sprintf("%s@%s is not installed", name, version),
				hint: "list installed versions with `paq ls`",
			}
		}
		return []state.InstalledApp{rec}, nil
	}

	if len(matches) == 1 {
		return matches, nil
	}

	// Multiple versions installed: require disambiguation.
	var versions []string
	for _, m := range matches {
		versions = append(versions, m.Version)
	}
	return nil, hintError{
		msg:  fmt.Sprintf("multiple versions of %q installed: %s", name, strings.Join(versions, ", ")),
		hint: fmt.Sprintf("specify one with %s@<version>", name),
	}
}

// printUninstallTargets prints the list of state entries that will be
// removed, under the given header. Shared by --dry-run and the confirmation prompt.
func printUninstallTargets(header string, targets []state.InstalledApp) {
	ui.Step("%s", header)
	for _, rec := range targets {
		ui.Step("  %s %s → %s", rec.Name, rec.Version, rec.Dest)
		if rec.Kind == "binaries" {
			for _, f := range rec.Files {
				ui.Step("    %s", f)
			}
		}
	}
}

// confirmYesNo prints prompt and reads a y/n answer from r. Returns true only
// for "y" or "yes" (case-insensitive); any other input (including empty)
// is treated as a refusal.
func confirmYesNo(r io.Reader, prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, _ := bufio.NewReader(r).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// survivingPaths returns the set of on-disk paths (cleaned) still owned by the
// records that remain after the given targets are removed. Used so uninstall
// never deletes files another installed record shares.
func survivingPaths(st *state.State, targets []state.InstalledApp) map[string]bool {
	doomed := make(map[string]bool, len(targets))
	for _, t := range targets {
		doomed[t.Name+"@"+t.Version] = true
	}
	paths := make(map[string]bool)
	for _, rec := range st.Packages {
		if doomed[rec.Name+"@"+rec.Version] {
			continue
		}
		for _, p := range rec.OwnedPaths() {
			paths[filepath.Clean(p)] = true
		}
	}
	return paths
}

// removeRecordFiles removes from the filesystem the files or directories
// installed for a state entry, based on its Kind. Paths present in keep are
// left in place (another installed record still owns them).
func removeRecordFiles(rec state.InstalledApp, keep map[string]bool) error {
	switch rec.Kind {
	case "file":
		if keep[filepath.Clean(rec.Dest)] {
			ui.Step("keeping %s (still used by another installed version)", rec.Dest)
			return nil
		}
		if err := os.Remove(rec.Dest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", rec.Dest, err)
		}
	case "dir":
		if keep[filepath.Clean(rec.Dest)] {
			ui.Step("keeping %s (still used by another installed version)", rec.Dest)
			return nil
		}
		info, statErr := os.Stat(rec.Dest)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil
			}
			return fmt.Errorf("stat %s: %w", rec.Dest, statErr)
		}
		if !info.IsDir() {
			if err := os.Remove(rec.Dest); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", rec.Dest, err)
			}
			return nil
		}
		if home, err := os.UserHomeDir(); err == nil {
			if clean := filepath.Clean(rec.Dest); clean == filepath.Clean(home) || clean == filepath.Dir(clean) {
				return fmt.Errorf("refusing to remove %s: not a paq-managed directory", rec.Dest)
			}
		}
		if err := os.RemoveAll(rec.Dest); err != nil {
			return fmt.Errorf("remove %s: %w", rec.Dest, err)
		}
	case "binaries":
		// Only remove the installed files, not the shared bin dir (e.g. ~/.local/bin).
		for _, p := range rec.Files {
			if keep[filepath.Clean(p)] {
				ui.Step("keeping %s (still used by another installed version)", p)
				continue
			}
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", p, err)
			}
		}
	default:
		return fmt.Errorf("unknown kind %q for %s", rec.Kind, rec.Name)
	}
	return nil
}

// parseAppRef splits a "name" or "name@version" reference.
func parseAppRef(ref string) (name, version string) {
	if i := strings.LastIndex(ref, "@"); i > 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}
