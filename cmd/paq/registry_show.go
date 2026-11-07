package main

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var registryShowCmd = &cobra.Command{
	Use:               "show <name>",
	Short:             "Show details of a single tool spec in the registry",
	Long:              "Show the full spec for a tool in the registry, regardless of whether it is in the user manifest.",
	Example:           `  paq registry show ripgrep`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeRegistrySpecs,
	RunE:              runRegistryShow,
}

func runRegistryShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	spec, ok := cfg.Specs[name]
	if !ok {
		return hintError{msg: fmt.Sprintf("spec %q not found in registry", name), hint: specNotFoundHint(cfg.Specs, name)}
	}

	return ui.PrintSpecDetail(name, spec)
}

// specNotFoundHint suggests the definitions the user may have meant by name,
// or how to browse the registry when nothing is close.
func specNotFoundHint(specs map[string]config.Spec, name string) string {
	if s := similarSpecs(specs, name); len(s) > 0 {
		return fmt.Sprintf("did you mean: %s?", strings.Join(s, ", "))
	}
	return "search the registry with `paq search <name>`, or list it all with `paq search`"
}

// similarSpecs returns the definition names that contain the query as a
// substring, or whose recipe installs an executable named query (so "rg"
// finds "ripgrep").
func similarSpecs(specs map[string]config.Spec, query string) []string {
	q := strings.ToLower(query)
	var out []string
	for name, spec := range specs {
		if strings.Contains(strings.ToLower(name), q) || installsExecutable(spec, q) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// installsExecutable reports whether spec extracts an executable whose name
// (without the {{ext}} suffix) is name.
func installsExecutable(spec config.Spec, name string) bool {
	files := []string{spec.Extract}
	for _, b := range spec.Binaries {
		files = append(files, b.From, b.To)
	}
	for _, f := range files {
		if f != "" && strings.ToLower(strings.TrimSuffix(path.Base(f), "{{ext}}")) == name {
			return true
		}
	}
	return false
}
