package main

import (
	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:     "registry",
	Aliases: []string{"reg"},
	Short:   "Inspect and update the registry of tool definitions",
	Long:    "Browse, show and update the tool definitions provided by the registry (embedded in the binary, optionally overlaid by a downloaded snapshot).",
	// With no subcommand, show the help.
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryShowCmd)
	rootCmd.AddCommand(registryCmd)
}
