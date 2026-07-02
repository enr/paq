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

	ui.PrintLsTable(st.Packages)
	return nil
}
