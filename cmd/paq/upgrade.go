package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/download"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/version"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:     "upgrade [app...]",
	Aliases: []string{"up"},
	Short:   "Upgrade installed tools to a newer version",
	Long: "Upgrade a tool (or all tools tracked in the manifest) pinned to \"latest\" " +
		"to the most recent upstream release. Tools pinned to a fixed version are left untouched.",
	Example: `  paq upgrade         # upgrade every "latest"-pinned app in the manifest
  paq upgrade rg      # upgrade a single app
  paq upgrade rg bat  # upgrade multiple apps`,
	Args: cobra.ArbitraryArgs,
	RunE: runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	// A single explicit app gets the friendlier single-app UX: no [name]
	// prefix on its output, and a progress bar for the download.
	if len(args) == 1 {
		name := args[0]
		if _, ok := cfg.Apps[name]; !ok {
			return fmt.Errorf("app %q not found in manifest (~/.config/paq/config.toml)", name)
		}
		return upgradeApp(ctx, cfg, name, appHooks(name, ""), ui.NewProgressFn(name))
	}

	var names []string
	if len(args) > 1 {
		// Validate every name before upgrading anything.
		for _, name := range args {
			if _, ok := cfg.Apps[name]; !ok {
				return fmt.Errorf("app %q not found in manifest (~/.config/paq/config.toml)", name)
			}
		}
		names = args
	} else {
		// No args: upgrade all apps from the manifest.
		if len(cfg.Apps) == 0 {
			ui.Info("No apps configured in manifest (~/.config/paq/config.toml)")
			return nil
		}
		for name := range cfg.Apps {
			names = append(names, name)
		}
	}

	var stdoutMu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)

	for _, name := range names {
		name := name // capture for the goroutine
		g.Go(func() error {
			prefix := fmt.Sprintf("[%-12s] ", name)
			hooks := lockedAppHooks(prefix, &stdoutMu)
			if err := upgradeApp(ctx, cfg, name, hooks, nil); err != nil {
				// Pipeline errors are already shown via OnFail; here we only
				// show errors specific to upgradeApp (e.g. reading state).
				if !install.ErrAlreadyShown(err) {
					stdoutMu.Lock()
					ui.Fail("%s%v", prefix, err)
					stdoutMu.Unlock()
				}
				return fmt.Errorf("%s: %w", name, err)
			}
			return nil
		})
	}

	return g.Wait()
}

// upgradeApp upgrades a single app: resolves the latest upstream version
// and, if different from the installed one, reinstalls and removes old versions.
func upgradeApp(ctx context.Context, cfg *config.Config, name string, hooks *install.Hooks, progress download.ProgressFn) error {
	// step shows a neutral message (in progress / skip); ok shows a positive
	// outcome. Both are visible by default (suppressed only by --quiet), so
	// the upgrade's outcome isn't silent without --verbose.
	step := func(format string, a ...any) {
		if hooks != nil && hooks.OnStep != nil {
			hooks.OnStep(fmt.Sprintf(format, a...))
		}
	}
	ok := func(format string, a ...any) {
		if hooks != nil && hooks.OnOK != nil {
			hooks.OnOK(fmt.Sprintf(format, a...))
		}
	}

	app := cfg.Apps[name]

	// Apps pinned to a fixed version are not upgraded.
	if strings.ToLower(app.Version) != "latest" {
		step("pinned to %s, skipping", app.Version)
		return nil
	}

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	installed := st.ByName(name)
	if len(installed) == 0 {
		step("not installed, skipping (use 'paq install %s')", name)
		return nil
	}

	specName := app.Use
	if specName == "" {
		specName = name
	}
	spec, found := cfg.Specs[specName]
	if !found {
		return fmt.Errorf("spec %q not found in registry", specName)
	}
	step("Resolving latest version...")
	latest, err := resolveLatestVersion(ctx, spec)
	if errors.Is(err, version.ErrLatestNotImplemented) {
		step("backend %q has no upstream version to resolve, skipping", spec.Backend)
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve latest version: %w", err)
	}

	// If the latest version is already installed, nothing to do.
	for _, rec := range installed {
		if rec.Version == latest {
			ok("already up to date (%s)", latest)
			return nil
		}
	}

	// Install the new version (the pipeline writes the new state record).
	if err := install.Run(ctx, cfg, name, progress, hooks); err != nil {
		return err
	}

	// Remove the old versions left in the state.
	return cleanupOldVersions(name, latest, installed, ok)
}

// latestRequestFor builds the version.LatestRequest for a spec's "latest"
// resolution. Shared by every command that needs to resolve or check the
// resolvability of "latest" (upgrade, outdated, import).
func latestRequestFor(spec config.Spec) version.LatestRequest {
	return version.LatestRequest{
		Strategy: spec.LatestStrategy,
		Backend:  spec.Backend,
		Repo:     spec.Repo,
		Source:   spec.Source,
		ArchPkg:  spec.ArchPkg,
	}
}

// resolveLatestVersion resolves the latest upstream version for a spec,
// selecting the provider from its backend/latest_strategy. Returns
// version.ErrLatestNotImplemented if neither can resolve "latest". Shared by
// upgradeApp and the "outdated" command.
func resolveLatestVersion(ctx context.Context, spec config.Spec) (string, error) {
	provider := version.LatestProvider(latestRequestFor(spec))
	latest, _, err := provider.Resolve(ctx)
	return latest, err
}

// cleanupOldVersions removes the state entries (and their files) for versions
// other than keepVersion after an upgrade. Files are removed only if the
// destination differs from the new version's: otherwise the pipeline has
// already overwritten the install in-place.
func cleanupOldVersions(name, keepVersion string, old []state.InstalledApp, ok func(string, ...any)) error {
	// Read the new record's dest to decide whether old files can be removed.
	// (If both versions installed to the same path the pipeline already overwrote
	// the files in-place; removing them would break the new install.)
	var newDest string
	if st, err := state.Load(); err == nil {
		if rec, ok := st.Get(name, keepVersion); ok {
			newDest = rec.Dest
		}
	}

	for _, rec := range old {
		if rec.Version == keepVersion {
			continue
		}
		if rec.Dest != newDest {
			if err := removeRecordFiles(rec); err != nil {
				return err
			}
		}
	}

	if err := state.Update(func(st *state.State) error {
		for _, rec := range old {
			if rec.Version != keepVersion {
				st.Delete(rec.Name, rec.Version)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	ok("upgraded to %s", keepVersion)
	return nil
}
