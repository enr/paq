package main

import (
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:               "search [query]",
	Aliases:           []string{"s"},
	Short:             "Search the registry for tool definitions",
	Long:              "Search the embedded registry for tool definitions whose name contains the given query; with no query, list them all. Shortcut for \"paq registry list [query]\".",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeRegistrySpecs,
	RunE: func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		return listDefinitions(query)
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
}
