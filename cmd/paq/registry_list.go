package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var registryListCmd = &cobra.Command{
	Use:     "list [query]",
	Aliases: []string{"ls"},
	Short:   "List tool definitions in the embedded registry",
	Long:    "List the tool specs bundled in the embedded registry, optionally filtered by a substring query on the spec name.",
	Example: `  paq registry list
  paq registry list jdk   # filter by substring on the name`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRegistryList,
}

func runRegistryList(cmd *cobra.Command, args []string) error {
	var query string
	if len(args) == 1 {
		query = args[0]
	}
	return listDefinitions(query)
}

// listDefinitions prints the registry definitions, filtered by substring
// match on the name if query is non-empty. Shared by "registry list" and "search".
func listDefinitions(query string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	q := strings.ToLower(query)

	rows := make([]ui.RegistryEntry, 0, len(cfg.Specs))
	for name, spec := range cfg.Specs {
		if q != "" && !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		rows = append(rows, ui.RegistryEntry{
			Name:    name,
			Backend: spec.Backend,
			Repo:    spec.Repo,
		})
	}

	if len(rows) == 0 && !ui.Global.JSON {
		if query != "" {
			fmt.Printf("No tool definitions matching %q in the embedded registry.\n", query)
		} else {
			fmt.Println("No tool definitions in the embedded registry.")
		}
		return nil
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name
	})

	ui.PrintAvailableTable(rows)
	return nil
}
