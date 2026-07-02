package main

import (
	"encoding/json"
	"fmt"

	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var whichCmd = &cobra.Command{
	Use:   "which <app[@version]>",
	Short: "Print the installed path(s) of a tool",
	Long: "Print the installed path(s) of a tool: the destination file/directory for " +
		"kind \"file\"/\"dir\", or one line per installed binary for kind \"binaries\". " +
		"If multiple versions are installed and no @version is given, prints all of them.",
	Example: `  paq which rg
  paq which rg@14.1.1`,
	Args: cobra.ExactArgs(1),
	RunE: runWhich,
}

func init() {
	rootCmd.AddCommand(whichCmd)
}

func runWhich(cmd *cobra.Command, args []string) error {
	name, version := parseAppRef(args[0])

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	var matches []state.InstalledApp
	if version != "" {
		rec, ok := st.Get(name, version)
		if !ok {
			return hintError{
				msg:  fmt.Sprintf("%s@%s is not installed", name, version),
				hint: "list installed versions with `paq ls`",
			}
		}
		matches = []state.InstalledApp{rec}
	} else {
		matches = st.ByName(name)
		if len(matches) == 0 {
			return hintError{
				msg:  fmt.Sprintf("%q is not installed", name),
				hint: "list installed tools with `paq ls`",
			}
		}
	}

	if ui.Global.JSON {
		data, _ := json.MarshalIndent(matches, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	for _, rec := range matches {
		if rec.Kind == "binaries" {
			for _, f := range rec.Files {
				fmt.Println(f)
			}
			continue
		}
		fmt.Println(rec.Dest)
	}
	return nil
}
