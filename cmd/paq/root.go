package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/httpretry"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

// jsonCapableCommands lists the full command paths that honor --json.
// Any other runnable command rejects --json instead of silently ignoring it.
var jsonCapableCommands = map[string]bool{
	"paq ls":              true,
	"paq registry list":   true,
	"paq registry show":   true,
	"paq registry status": true,
	"paq info":            true,
	"paq config show":     true,
	"paq import":          true,
	"paq search":          true,
	"paq outdated":        true,
	"paq audit":           true,
	"paq which":           true,
	"paq doctor":          true,
}

var (
	flagNoColor bool
	flagJSON    bool
	flagQuiet   bool
	flagVerbose bool
	flagDebug   bool
	flagConfig  string
	flagLocal   bool
)

var rootCmd = &cobra.Command{
	Use:   "paq",
	Short: "paq — install CLI tools from GitHub releases and URLs",
	Long:  `paq installs and manages CLI tools defined in a registry, downloading from GitHub releases, direct URLs or the npm registry.`,
	// Error and usage printing is handled centrally in Execute, so that
	// colors and consistent hints can be added.
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		ui.Global = ui.Config{
			NoColor: flagNoColor || os.Getenv("NO_COLOR") != "",
			JSON:    flagJSON,
			Quiet:   flagQuiet,
			// --debug implies --verbose output.
			Verbose: flagVerbose || flagDebug,
			Debug:   flagDebug,
		}
		if flagJSON && cmd.Runnable() && !jsonCapableCommands[cmd.CommandPath()] {
			return fmt.Errorf("--json is not supported by %q", cmd.CommandPath())
		}
		if err := applyConfigPathOverride(); err != nil {
			return err
		}
		return nil
	},
	// Runs only after a successful command: never delays the work and never
	// nags on failure.
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		maybeNotifyUpdate(cmd)
		return nil
	},
}

func Execute() {
	// Handle Ctrl-C (and SIGTERM) by cancelling the command's context instead
	// of letting the runtime kill the process: the commands then unwind their
	// defers, releasing the state lock and removing temp files and half-written
	// destinations. A second signal restores the default behaviour, so an
	// operation that does not observe the cancellation can still be killed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	requireCompletionShell(rootCmd)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		reportError(err)
		os.Exit(exitCodeFor(err))
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable color output")
	rootCmd.PersistentFlags().BoolVarP(&flagJSON, "json", "j", false, "Output as JSON")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress non-essential output")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "Print detailed debug output to stderr (implies --verbose)")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "Path to the manifest file (default: ~/.config/paq/config.toml, or $PAQ_CONFIG)")
	rootCmd.PersistentFlags().BoolVar(&flagLocal, "local", false, "Project-local mode: use the nearest paq.toml and keep state and default install dirs under .paq/ next to it (also $PAQ_LOCAL)")

	// Surface httpretry's retries under --debug: reads ui.Global.Debug at call
	// time (set once here, evaluated on every retry), so a slow or flaky
	// network no longer looks like paq just going quiet for a while.
	httpretry.OnRetry = func(attempt int, resp *http.Response, err error, delay time.Duration) {
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests && os.Getenv("GITHUB_TOKEN") == "" {
			ui.Warn("rate-limited (attempt %d) — retrying in %s; set GITHUB_TOKEN to avoid this", attempt, delay)
		}
		if !ui.Global.Debug {
			return
		}
		if err != nil {
			ui.Debug("HTTP request failed (attempt %d): %v — retrying in %s", attempt, err, delay)
			return
		}
		ui.Debug("HTTP request got status %d (attempt %d) — retrying in %s", resp.StatusCode, attempt, delay)
	}
}

// applyConfigPathOverride sets config.PathOverride from --config, falling
// back to PAQ_CONFIG when the flag is not set, so a manifest can live outside
// the XDG-derived default (multiple profiles, CI, a dotfiles-managed path).
// Always assigns (clearing the override when neither is set) so the package
// global doesn't leak a stale value across commands within the same process.
//
// In local mode (--local, or PAQ_LOCAL set to any value but "", "0" or
// "false") the manifest is the nearest paq.toml instead, and the state DB and
// the default install dirs move under .paq/ next to it. PAQ_CONFIG is ignored
// there; an explicit --config conflicts with it.
func applyConfigPathOverride() error {
	config.ProjectRoot, state.PathOverride = "", ""
	if localMode() {
		if flagConfig != "" {
			return fmt.Errorf("--config cannot be combined with --local")
		}
		root, err := findProjectRoot()
		if err != nil {
			return fmt.Errorf("resolve --local: %w", err)
		}
		config.ProjectRoot = root
		config.PathOverride = filepath.Join(root, localManifestName)
		state.PathOverride = filepath.Join(root, ".paq", "state.json")
		return nil
	}

	path := flagConfig
	if path == "" {
		path = os.Getenv("PAQ_CONFIG")
	}
	if path == "" {
		config.PathOverride = ""
		return nil
	}
	resolved, err := expandHome(path)
	if err != nil {
		return fmt.Errorf("resolve --config: %w", err)
	}
	config.PathOverride = resolved
	return nil
}

// localManifestName is the manifest file that marks a project root in local mode.
const localManifestName = "paq.toml"

// localMode reports whether local mode is on: --local, or PAQ_LOCAL set to
// any value but "", "0" or "false".
func localMode() bool {
	if flagLocal {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PAQ_LOCAL"))) {
	case "", "0", "false":
		return false
	}
	return true
}

// findProjectRoot returns the nearest directory, from the working directory
// upwards, that holds a paq.toml; the working directory itself when none does,
// so a first `paq --local install <tool>` creates the manifest there.
func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, localManifestName)); err == nil {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			return cwd, nil
		}
	}
}
