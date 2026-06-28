package main

import (
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/enr/paq/embedded"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
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
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	if len(args) == 1 {
		hooks := appHooks(args[0], "")
		progress := ui.NewProgressFn(args[0])
		return install.Run(ctx, cfg, args[0], progress, hooks)
	}

	// Installa tutte le app del manifest in parallelo (max maxParallel goroutine)
	if len(cfg.Apps) == 0 {
		fmt.Println("No apps configured in manifest (~/.config/paq/config.toml)")
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
