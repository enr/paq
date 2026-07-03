package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/enr/paq/embedded"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var (
	flagInstallForce  bool
	flagInstallNoSave bool
)

const maxParallel = 3

var installCmd = &cobra.Command{
	Use:     "install [app...]",
	Aliases: []string{"i"},
	Short:   "Install a tool (or all tools from manifest if no app specified)",
	Example: `  paq install ripgrep            # install, recording it in the manifest
  paq install ripgrep --no-save  # install without recording it (ephemeral)
  paq install ripgrep bat delta  # install multiple tools
  paq install                    # install every tool from the manifest`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completeInstallableNames,
	RunE:              runInstall,
}

func init() {
	installCmd.Flags().BoolVarP(&flagInstallForce, "force", "f", false, "Reinstall even if already installed")
	installCmd.Flags().BoolVar(&flagInstallNoSave, "no-save", false, "Install without recording the tool in the manifest (ephemeral)")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	// A single explicit app gets the friendlier single-app UX: no [name]
	// prefix on its output, and a progress bar for the download.
	if len(args) == 1 {
		name := args[0]
		path, err := ensureManifestEntry(cfg, name, !flagInstallNoSave)
		if err != nil {
			return err
		}
		if path != "" {
			ui.OK("added %s to %s", name, path)
		}
		hooks := appHooks(name, "")
		hooks.Force = flagInstallForce
		progress := ui.NewProgressFn(name)
		return install.Run(ctx, cfg, name, progress, hooks)
	}

	if len(args) > 1 {
		// Validate every name before touching the manifest or installing
		// anything, so a typo in the last argument doesn't leave earlier
		// apps auto-imported into the manifest.
		for _, name := range args {
			if err := validateAppName(cfg, name); err != nil {
				return err
			}
		}
		for _, name := range args {
			path, err := ensureManifestEntry(cfg, name, !flagInstallNoSave)
			if err != nil {
				return err
			}
			if path != "" {
				ui.OK("added %s to %s", name, path)
			}
		}
		return installParallel(ctx, cfg, args)
	}

	// No args: install all apps from the manifest.
	if len(cfg.Apps) == 0 {
		ui.Info("No apps configured in manifest (~/.config/paq/config.toml)")
		return nil
	}
	names := make([]string, 0, len(cfg.Apps))
	for name := range cfg.Apps {
		names = append(names, name)
	}
	return installParallel(ctx, cfg, names)
}

// installParallel installs names concurrently (max maxParallel goroutines),
// each with a [name]-prefixed, mutex-serialized set of hooks so output from
// different goroutines doesn't interleave.
func installParallel(ctx context.Context, cfg *config.Config, names []string) error {
	var stdoutMu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)

	for _, name := range names {
		name := name // capture for the goroutine
		g.Go(func() error {
			prefix := fmt.Sprintf("[%-12s] ", name)
			lockedHooks := lockedAppHooks(prefix, &stdoutMu)
			lockedHooks.Force = flagInstallForce
			if err := install.Run(ctx, cfg, name, nil, lockedHooks); err != nil {
				// The pipeline already shows the error via OnFail; we only
				// reprint it if for some reason it wasn't shown.
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

// ensureManifestEntry ensures that cfg.Apps[name] exists, so that install
// can proceed. If name is missing from the manifest but matches a registry
// spec, it synthesizes a default entry (auto-import): injects it into
// cfg.Apps in memory and, if save is true, persists it to the user manifest.
// Returns the written path ("" if not persisted or the app already existed)
// or a hintError if name is neither a known app nor a known spec.
func ensureManifestEntry(cfg *config.Config, name string, save bool) (string, error) {
	if _, exists := cfg.Apps[name]; exists {
		return "", nil
	}

	if err := validateAppName(cfg, name); err != nil {
		return "", err
	}

	spec := cfg.Specs[name]
	entry := config.AppEntry{
		Use:     name,
		Version: defaultImportVersion(spec),
		Dest:    config.DefaultDest(spec, name, cfg.Defaults),
	}
	// Makes the app installable in memory, regardless of persistence.
	cfg.Apps[name] = entry

	if !save {
		return "", nil
	}

	block := renderAppEntryTOML(name, entry)
	path, err := config.WriteManifestEntry(name, block, false)
	if err != nil {
		return "", fmt.Errorf("write manifest entry: %w", err)
	}
	return path, nil
}

// validateAppName reports whether name can be installed: either it's already
// in the manifest, or it matches a known registry spec with a valid app key.
// Used to fail fast on a typo before installing or auto-importing anything.
func validateAppName(cfg *config.Config, name string) error {
	if _, exists := cfg.Apps[name]; exists {
		return nil
	}
	if _, ok := cfg.Specs[name]; !ok {
		hint := "list available definitions with `paq registry`"
		if s := similarSpecs(cfg.Specs, name); len(s) > 0 {
			hint = fmt.Sprintf("did you mean: %s?", strings.Join(s, ", "))
		}
		return hintError{
			msg:  fmt.Sprintf("%q is not in your manifest and not a known registry spec", name),
			hint: hint,
		}
	}
	if !validAppKey(name) {
		return fmt.Errorf("invalid app name %q: use only letters, digits, '-' or '_'", name)
	}
	return nil
}

// appHooks builds the Hooks for a single app (used for single-app install).
func appHooks(name, prefix string) *install.Hooks {
	return &install.Hooks{
		OnStep:  func(msg string) { ui.Step("%s%s", prefix, msg) },
		OnOK:    func(msg string) { ui.OK("%s%s", prefix, msg) },
		OnFail:  func(err error) { ui.Fail("%s%v", prefix, err) },
		OnInfo:  func(msg string) { ui.Info("%s%s", prefix, msg) },
		OnWarn:  func(msg string) { ui.Warn("%s%s", prefix, msg) },
		OnDebug: func(msg string) { ui.Debug("%s%s", prefix, msg) },
	}
}

// lockedAppHooks builds Hooks serialized on a shared mutex, to avoid
// interleaved output during parallel operations (install/upgrade of multiple apps).
func lockedAppHooks(prefix string, mu *sync.Mutex) *install.Hooks {
	return &install.Hooks{
		OnStep:  func(msg string) { mu.Lock(); ui.Step("%s%s", prefix, msg); mu.Unlock() },
		OnOK:    func(msg string) { mu.Lock(); ui.OK("%s%s", prefix, msg); mu.Unlock() },
		OnFail:  func(err error) { mu.Lock(); ui.Fail("%s%v", prefix, err); mu.Unlock() },
		OnInfo:  func(msg string) { mu.Lock(); ui.Info("%s%s", prefix, msg); mu.Unlock() },
		OnWarn:  func(msg string) { mu.Lock(); ui.Warn("%s%s", prefix, msg); mu.Unlock() },
		OnDebug: func(msg string) { mu.Lock(); ui.Debug("%s%s", prefix, msg); mu.Unlock() },
	}
}

// loadConfig loads the registry + global templates + user manifest and merges them.
func loadConfig() (*config.Config, error) {
	cfg, _, err := loadConfigWithMeta()
	return cfg, err
}

// loadConfigWithMeta is loadConfig plus the metadata of the external registry
// snapshot in use (nil when running on the embedded registry only).
// A missing or broken snapshot never fails the load: it degrades to the
// embedded registry with a warning on stderr. No network is ever involved.
func loadConfigWithMeta() (*config.Config, *registry.Meta, error) {
	specs, err := config.LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		return nil, nil, fmt.Errorf("load registry: %w", err)
	}

	globalTmpl, globalTmplOS, err := config.LoadGlobalTemplates(embedded.RegistryFS)
	if err != nil {
		return nil, nil, fmt.Errorf("load global templates: %w", err)
	}

	var regMeta *registry.Meta
	snapFS, meta, err := registry.Open()
	if err != nil {
		ui.Warn("external registry cache is unusable (%v): using the embedded registry, run `paq registry update` to refresh it", err)
	} else if snapFS != nil {
		if err := config.OverlayRegistry(specs, globalTmpl, globalTmplOS, snapFS); err != nil {
			ui.Warn("external registry cache is unusable (%v): using the embedded registry, run `paq registry update` to refresh it", err)
		} else {
			regMeta = meta
		}
	}

	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("load user config: %w", err)
	}
	userCfg.GlobalTemplates = globalTmpl
	userCfg.GlobalTemplatesOS = globalTmplOS

	cfg, err := config.Merge(specs, userCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("merge config: %w", err)
	}
	return cfg, regMeta, nil
}
