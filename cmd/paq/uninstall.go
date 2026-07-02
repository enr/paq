package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var (
	flagUninstallDryRun bool
	flagUninstallYes    bool
)

var uninstallCmd = &cobra.Command{
	Use:     "uninstall <app[@version]>",
	Aliases: []string{"rm", "remove"},
	Short:   "Uninstall a tool (use app@version to disambiguate multiple versions)",
	Args:    cobra.ExactArgs(1),
	RunE:    runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVar(&flagUninstallDryRun, "dry-run", false, "Show what would be removed without removing anything")
	uninstallCmd.Flags().BoolVarP(&flagUninstallYes, "yes", "y", false, "Skip confirmation prompt")
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	name, version := parseAppRef(args[0])

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	matches := st.ByName(name)
	if len(matches) == 0 {
		return hintError{
			msg:  fmt.Sprintf("%q is not installed", name),
			hint: "list installed tools with `paq ls`",
		}
	}

	// Select the entries to remove.
	var targets []state.InstalledApp
	if version != "" {
		rec, ok := st.Get(name, version)
		if !ok {
			return hintError{
				msg:  fmt.Sprintf("%s@%s is not installed", name, version),
				hint: "list installed versions with `paq ls`",
			}
		}
		targets = []state.InstalledApp{rec}
	} else if len(matches) == 1 {
		targets = matches
	} else {
		// Multiple versions installed: require disambiguation.
		var versions []string
		for _, m := range matches {
			versions = append(versions, m.Version)
		}
		return hintError{
			msg:  fmt.Sprintf("multiple versions of %q installed: %s", name, strings.Join(versions, ", ")),
			hint: fmt.Sprintf("specify one with %s@<version>", name),
		}
	}

	if flagUninstallDryRun {
		printUninstallTargets("dry-run: would remove:", targets)
		return nil
	}

	// Ask for confirmation unless --yes was passed or stdout is not a
	// terminal (non-interactive/CI invocations proceed without prompting).
	if !flagUninstallYes && ui.IsTTY() {
		printUninstallTargets("This will remove:", targets)
		if !confirmYesNo(os.Stdin, "Continue?") {
			ui.Info("aborted")
			return nil
		}
	}

	for _, rec := range targets {
		ui.Step("Uninstalling %s %s from %s...", rec.Name, rec.Version, rec.Dest)
		if err := removeRecordFiles(rec); err != nil {
			return err
		}
		st.Delete(rec.Name, rec.Version)
	}

	if err := st.Save(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	ui.OK("%s uninstalled", name)
	return nil
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

// removeRecordFiles removes from the filesystem the files or directories
// installed for a state entry, based on its Kind.
func removeRecordFiles(rec state.InstalledApp) error {
	switch rec.Kind {
	case "file":
		if err := os.Remove(rec.Dest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", rec.Dest, err)
		}
	case "dir":
		if err := os.RemoveAll(rec.Dest); err != nil {
			return fmt.Errorf("remove %s: %w", rec.Dest, err)
		}
	case "binaries":
		// Only remove the installed files, not the shared bin dir (e.g. ~/.local/bin).
		for _, p := range rec.Files {
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
