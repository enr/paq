package main

import (
	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:     "registry",
	Aliases: []string{"reg"},
	Short:   "Inspect the embedded registry of tool definitions",
	Long:    "Browse and show the tool definitions bundled in the embedded registry.",
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
