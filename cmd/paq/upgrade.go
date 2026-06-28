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
	Use:     "upgrade [app]",
	Aliases: []string{"up"},
	Short:   "Upgrade installed tools to a newer version",
	Long: "Upgrade a tool (or all tools tracked in the manifest) pinned to \"latest\" " +
		"to the most recent upstream release. Tools pinned to a fixed version are left untouched.",
	Args: cobra.MaximumNArgs(1),
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

	// Upgrade di una singola app.
	if len(args) == 1 {
		name := args[0]
		if _, ok := cfg.Apps[name]; !ok {
			return fmt.Errorf("app %q not found in manifest (~/.config/paq/config.toml)", name)
		}
		return upgradeApp(ctx, cfg, name, appHooks(name, ""), ui.NewProgressFn(name))
	}

	// Upgrade di tutte le app del manifest in parallelo.
	if len(cfg.Apps) == 0 {
		fmt.Println("No apps configured in manifest (~/.config/paq/config.toml)")
		return nil
	}

	var stdoutMu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)

	for name := range cfg.Apps {
		name := name // cattura per la goroutine
		g.Go(func() error {
			prefix := fmt.Sprintf("[%-12s] ", name)
			hooks := lockedAppHooks(prefix, &stdoutMu)
			if err := upgradeApp(ctx, cfg, name, hooks, nil); err != nil {
				// Errori della pipeline sono già mostrati via OnFail; mostriamo qui
				// solo quelli propri di upgradeApp (es. lettura stato).
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

// upgradeApp aggiorna una singola app: risolve la versione upstream più recente
// e, se diversa da quella installata, reinstalla e rimuove le versioni vecchie.
func upgradeApp(ctx context.Context, cfg *config.Config, name string, hooks *install.Hooks, progress download.ProgressFn) error {
	info := func(format string, a ...any) {
		if hooks != nil && hooks.OnInfo != nil {
			hooks.OnInfo(fmt.Sprintf(format, a...))
		}
	}

	app := cfg.Apps[name]

	// Le app pinnate a una versione fissa non vengono aggiornate.
	if strings.ToLower(app.Version) != "latest" {
		info("pinned to %s, skipping", app.Version)
		return nil
	}

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	installed := st.ByName(name)
	if len(installed) == 0 {
		info("not installed, skipping (use 'paq install %s')", name)
		return nil
	}

	specName := app.Use
	if specName == "" {
		specName = name
	}
	spec, ok := cfg.Specs[specName]
	if !ok {
		return fmt.Errorf("spec %q not found in registry", specName)
	}
	info("Resolving latest version...")
	provider := version.LatestProvider(version.LatestRequest{
		Strategy: spec.LatestStrategy,
		Backend:  spec.Backend,
		Repo:     spec.Repo,
		Source:   spec.Source,
		ArchPkg:  spec.ArchPkg,
	})
	latest, _, err := provider.Resolve(ctx)
	if errors.Is(err, version.ErrLatestNotImplemented) {
		info("backend %q has no upstream version to resolve, skipping", spec.Backend)
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve latest version: %w", err)
	}

	// Se la versione più recente è già installata, niente da fare.
	for _, rec := range installed {
		if rec.Version == latest {
			info("already up to date (%s)", latest)
			return nil
		}
	}

	// Installa la nuova versione (la pipeline scrive il nuovo record di stato).
	if err := install.Run(ctx, cfg, name, progress, hooks); err != nil {
		return err
	}

	// Rimuovi le versioni precedenti rimaste nello stato.
	return cleanupOldVersions(name, latest, installed, info)
}

// cleanupOldVersions rimuove le entry di stato (e i relativi file) delle versioni
// diverse da keepVersion dopo un upgrade. I file vengono rimossi solo se la
// destinazione differisce da quella della nuova versione: in caso contrario la
// pipeline ha già sovrascritto l'installazione in-place.
func cleanupOldVersions(name, keepVersion string, old []state.InstalledApp, info func(string, ...any)) error {
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	newRec, _ := st.Get(name, keepVersion)

	for _, rec := range old {
		if rec.Version == keepVersion {
			continue
		}
		if rec.Dest != newRec.Dest {
			if err := removeRecordFiles(rec); err != nil {
				return err
			}
		}
		st.Delete(rec.Name, rec.Version)
	}

	if err := st.Save(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	info("upgraded to %s", keepVersion)
	return nil
}
