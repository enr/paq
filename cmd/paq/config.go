package main

import (
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect paq's evaluated configuration",
	Long:  "Inspect the user configuration (~/.config/paq/config.toml) as paq evaluates it.",
	// With no subcommand, show the help.
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	configCmd.AddCommand(configShowCmd)
	rootCmd.AddCommand(configCmd)
}
