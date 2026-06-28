package main

import (
	"os"
	"sort"
	"strings"

	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var registryShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show details of a single tool spec in the embedded registry",
	Long:  "Show the full spec for a tool in the embedded registry, regardless of whether it is in the user manifest.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistryShow,
}

func runRegistryShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	spec, ok := cfg.Specs[name]
	if !ok {
		ui.Fail("spec %q not found in registry", name)
		if suggestions := similarSpecs(cfg.Specs, name); len(suggestions) > 0 {
			ui.Hint("did you mean: %s?", strings.Join(suggestions, ", "))
		} else {
			ui.Hint("list available definitions with `paq registry`")
		}
		os.Exit(1)
	}

	ui.PrintSpecDetail(name, spec)
	return nil
}

// similarSpecs ritorna i nomi di definizione che contengono la query come sottostringa.
func similarSpecs[T any](specs map[string]T, query string) []string {
	q := strings.ToLower(query)
	var out []string
	for name := range specs {
		if strings.Contains(strings.ToLower(name), q) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
