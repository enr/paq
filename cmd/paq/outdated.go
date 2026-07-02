package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/version"
	"github.com/spf13/cobra"
)

var outdatedCmd = &cobra.Command{
	Use:   "outdated",
	Short: "List installed tools that have a newer upstream version",
	Long: "Check, for every installed app pinned to \"latest\", whether a newer upstream " +
		"version is available, without installing it. Apps pinned to a fixed version, not " +
		"installed, or whose backend cannot resolve \"latest\" are skipped (shown with --verbose).",
	Args: cobra.NoArgs,
	RunE: runOutdated,
}

func init() {
	rootCmd.AddCommand(outdatedCmd)
}

func runOutdated(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Apps) == 0 {
		if !ui.Global.JSON {
			fmt.Println("No apps configured in manifest (~/.config/paq/config.toml).")
			return nil
		}
		ui.PrintOutdatedTable([]ui.OutdatedEntry{})
		return nil
	}

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	ctx := cmd.Context()
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)

	var mu sync.Mutex
	results := []ui.OutdatedEntry{}
	skip := func(format string, a ...any) {
		mu.Lock()
		ui.Info(format, a...)
		mu.Unlock()
	}

	for name := range cfg.Apps {
		name := name // capture for the goroutine
		g.Go(func() error {
			entry, checked, err := checkOutdated(ctx, cfg, st, name, skip)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if !checked {
				return nil
			}
			mu.Lock()
			results = append(results, entry)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	ui.PrintOutdatedTable(results)
	return nil
}

// checkOutdated resolves the latest upstream version for a single app and
// reports whether it differs from what's installed. checked is false when
// the app was skipped (pinned, not installed, or no "latest" strategy) —
// skip reasons are reported through the skip callback, gated by --verbose
// the same way ui.Info always is.
func checkOutdated(ctx context.Context, cfg *config.Config, st *state.State, name string, skip func(string, ...any)) (entry ui.OutdatedEntry, checked bool, err error) {
	app := cfg.Apps[name]

	if !strings.EqualFold(app.Version, "latest") {
		skip("%s: pinned to %s, skipping", name, app.Version)
		return entry, false, nil
	}

	installed := st.ByName(name)
	if len(installed) == 0 {
		skip("%s: not installed, skipping", name)
		return entry, false, nil
	}

	specName := app.Use
	if specName == "" {
		specName = name
	}
	spec, found := cfg.Specs[specName]
	if !found {
		return entry, false, fmt.Errorf("spec %q not found in registry", specName)
	}

	latest, resolveErr := resolveLatestVersion(ctx, spec)
	if errors.Is(resolveErr, version.ErrLatestNotImplemented) {
		skip("%s: backend %q has no upstream version to resolve, skipping", name, spec.Backend)
		return entry, false, nil
	}
	if resolveErr != nil {
		return entry, false, fmt.Errorf("resolve latest version: %w", resolveErr)
	}

	entry, isOutdated := evaluateOutdated(name, installed, latest)
	return entry, isOutdated, nil
}

// evaluateOutdated decides whether name is outdated given its installed
// records and the resolved latest version: outdated means none of the
// installed versions match latest. Kept separate from checkOutdated (which
// does the network I/O to resolve latest) so the selection logic itself is
// unit-testable without a real version provider.
func evaluateOutdated(name string, installed []state.InstalledApp, latest string) (entry ui.OutdatedEntry, isOutdated bool) {
	installedVersions := make([]string, len(installed))
	for i, rec := range installed {
		installedVersions[i] = rec.Version
		if rec.Version == latest {
			return ui.OutdatedEntry{}, false // already up to date
		}
	}
	return ui.OutdatedEntry{
		Name:      name,
		Installed: strings.Join(installedVersions, ", "),
		Latest:    latest,
	}, true
}
