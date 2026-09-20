package main

import (
	"encoding/json"
	"fmt"
	"strings"

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
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeInstalledApps,
	RunE:              runWhich,
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

	// which feeds scripts (`$(paq which rg)`), so a path that no longer exists
	// must never be printed as though the tool were installed: the failure
	// would surface later, somewhere else, as a confusing "no such file".
	var present []state.InstalledApp
	var missing []string
	for _, rec := range matches {
		if gone := rec.MissingPaths(); len(gone) > 0 {
			missing = append(missing, gone...)
			continue
		}
		present = append(present, rec)
	}
	if len(missing) > 0 {
		ui.Warn("recorded but missing on disk: %s", strings.Join(missing, ", "))
	}
	if len(present) == 0 {
		return hintError{
			msg:  fmt.Sprintf("%q is recorded as installed but its files are missing", name),
			hint: fmt.Sprintf("reinstall it with `paq install %s`, or drop the record with `paq uninstall %s`", name, name),
		}
	}
	matches = present

	if ui.Global.JSON {
		data, err := json.MarshalIndent(matches, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal matches: %w", err)
		}
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
