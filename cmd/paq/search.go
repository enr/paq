package main

import (
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:     "search <query>",
	Aliases: []string{"s"},
	Short:   "Search the registry for tool definitions",
	Long:  "Search the embedded registry for tool definitions whose name contains the given query. Shortcut for \"paq registry list <query>\".",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return listDefinitions(args[0])
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
}
