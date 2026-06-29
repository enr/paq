package main

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/enr/paq/embedded"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var (
	flagInstallForce  bool
	flagInstallNoSave bool
)

const maxParallel = 3

var installCmd = &cobra.Command{
	Use:     "install [app]",
	Aliases: []string{"i"},
	Short:   "Install a tool (or all tools from manifest if no app specified)",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runInstall,
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

	// Installa tutte le app del manifest in parallelo (max maxParallel goroutine)
	if len(cfg.Apps) == 0 {
		ui.Info("No apps configured in manifest (~/.config/paq/config.toml)")
		return nil
	}

	// stdoutMu serializza le scritture su stdout/stderr per evitare output mescolato
	var stdoutMu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)

	for name := range cfg.Apps {
		name := name // cattura per la goroutine
		g.Go(func() error {
			prefix := fmt.Sprintf("[%-12s] ", name)
			lockedHooks := lockedAppHooks(prefix, &stdoutMu)
			lockedHooks.Force = flagInstallForce
			if err := install.Run(ctx, cfg, name, nil, lockedHooks); err != nil {
				// La pipeline mostra già l'errore via OnFail; lo ristampiamo solo
				// se per qualche motivo non è stato mostrato.
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

// ensureManifestEntry garantisce che cfg.Apps[name] esista, così che install
// possa procedere. Se name manca dal manifest ma corrisponde a una spec del
// registry, sintetizza una entry di default (auto-import): la inietta in
// cfg.Apps in memoria e — se save è true — la persiste nel manifest utente.
// Ritorna il path scritto ("" se non persistito o se l'app era già presente)
// oppure un hintError se name non è né un'app né una spec nota.
func ensureManifestEntry(cfg *config.Config, name string, save bool) (string, error) {
	if _, exists := cfg.Apps[name]; exists {
		return "", nil
	}

	spec, ok := cfg.Specs[name]
	if !ok {
		hint := "list available definitions with `paq registry`"
		if s := similarSpecs(cfg.Specs, name); len(s) > 0 {
			hint = fmt.Sprintf("did you mean: %s?", strings.Join(s, ", "))
		}
		return "", hintError{
			msg:  fmt.Sprintf("%q is not in your manifest and not a known registry spec", name),
			hint: hint,
		}
	}

	if !validAppKey(name) {
		return "", fmt.Errorf("invalid app name %q: use only letters, digits, '-' or '_'", name)
	}

	entry := config.AppEntry{
		Use:     name,
		Version: defaultImportVersion(spec),
		Dest:    config.DefaultDest(spec, name, cfg.Defaults),
	}
	// Rende l'app installabile in memoria, a prescindere dalla persistenza.
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

// appHooks costruisce gli Hooks per un singolo app (usato per install singola).
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

// lockedAppHooks costruisce Hooks serializzati su un mutex condiviso, per evitare
// output mescolato durante le operazioni parallele (install/upgrade di più app).
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

// loadConfig carica registry + template globali + manifest utente e li unisce.
func loadConfig() (*config.Config, error) {
	registry, err := config.LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}

	globalTmpl, globalTmplOS, err := config.LoadGlobalTemplates(embedded.RegistryFS)
	if err != nil {
		return nil, fmt.Errorf("load global templates: %w", err)
	}

	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return nil, fmt.Errorf("load user config: %w", err)
	}
	userCfg.GlobalTemplates = globalTmpl
	userCfg.GlobalTemplatesOS = globalTmplOS

	cfg, err := config.Merge(registry, userCfg)
	if err != nil {
		return nil, fmt.Errorf("merge config: %w", err)
	}
	return cfg, nil
}
