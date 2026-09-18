package main

import (
	"fmt"

	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List installed tools",
	Args:    cobra.NoArgs,
	RunE:    runLs,
}

func init() {
	rootCmd.AddCommand(lsCmd)
}

func runLs(cmd *cobra.Command, args []string) error {
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	if len(st.Packages) == 0 && !ui.Global.JSON {
		fmt.Println("No tools installed.")
		return nil
	}

	// Reconcile against the filesystem: a record whose files are gone would
	// otherwise be listed as installed, and nothing in paq would say otherwise.
	entries := make([]ui.LsEntry, len(st.Packages))
	missing := 0
	for i, rec := range st.Packages {
		entries[i] = ui.LsEntry{InstalledApp: rec, Missing: len(rec.MissingPaths()) > 0}
		if entries[i].Missing {
			missing++
		}
	}

	ui.PrintLsTable(entries)

	// The table keeps its shape; the drift is reported next to it. In --json
	// mode each entry already carries "missing", so the warning would be noise.
	if missing > 0 && !ui.Global.JSON {
		ui.Warn("%d of %d tools are recorded but missing on disk", missing, len(entries))
		ui.Hint("run `paq doctor` for details, or reinstall them with `paq install <name>`")
	}
	return nil
}
