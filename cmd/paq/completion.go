package main

import (
	"sort"
	"strings"

	"github.com/enr/paq/internal/state"
	"github.com/spf13/cobra"
)

// excludeArgs removes from candidates any value already present in already,
// so shell completion doesn't keep re-suggesting a name the user already typed.
func excludeArgs(candidates []string, already []string) []string {
	if len(already) == 0 {
		return candidates
	}
	seen := make(map[string]bool, len(already))
	for _, a := range already {
		seen[a] = true
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if !seen[c] {
			out = append(out, c)
		}
	}
	return out
}

// filterByPrefix keeps only the candidates starting with toComplete. Cobra's
// ValidArgsFunction contract does not filter its own output by the partial
// word being completed, so completion functions must do it themselves (the
// same convention kubectl and other cobra-based CLIs follow).
func filterByPrefix(candidates []string, toComplete string) []string {
	if toComplete == "" {
		return candidates
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if strings.HasPrefix(c, toComplete) {
			out = append(out, c)
		}
	}
	return out
}

// completeManifestApps completes with the app names configured in the user
// manifest (~/.config/paq/config.toml): used by commands that only operate
// on apps already tracked there (upgrade, info).
func completeManifestApps(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	names := make([]string, 0, len(cfg.Apps))
	for name := range cfg.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return excludeArgs(filterByPrefix(names, toComplete), args), cobra.ShellCompDirectiveNoFileComp
}

// completeInstallableNames completes with everything `paq install <name>`
// accepts: apps already in the manifest, plus any registry spec name (which
// install auto-imports on the fly).
func completeInstallableNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	seen := make(map[string]bool, len(cfg.Apps)+len(cfg.Specs))
	names := make([]string, 0, len(cfg.Apps)+len(cfg.Specs))
	for name := range cfg.Apps {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for name := range cfg.Specs {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return excludeArgs(filterByPrefix(names, toComplete), args), cobra.ShellCompDirectiveNoFileComp
}

// completeRegistrySpecs completes with the tool definitions in the embedded
// (+ user-defined) registry: used by commands that take a spec name
// (import, registry show) or filter on one (search, registry list).
func completeRegistrySpecs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	names := make([]string, 0, len(cfg.Specs))
	for name := range cfg.Specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return excludeArgs(filterByPrefix(names, toComplete), args), cobra.ShellCompDirectiveNoFileComp
}

// completeInstalledApps completes with the names of installed apps, plus
// "name@version" for any app with more than one version installed — the
// exact set `paq uninstall`/`paq which` accept.
func completeInstalledApps(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	st, err := state.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	versionsByName := make(map[string][]string)
	for _, rec := range st.Packages {
		versionsByName[rec.Name] = append(versionsByName[rec.Name], rec.Version)
	}

	names := make([]string, 0, len(versionsByName))
	for name := range versionsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, name)
		if versions := versionsByName[name]; len(versions) > 1 {
			sort.Strings(versions)
			for _, v := range versions {
				out = append(out, name+"@"+v)
			}
		}
	}
	return excludeArgs(filterByPrefix(out, toComplete), args), cobra.ShellCompDirectiveNoFileComp
}
