package main

import (
	"fmt"
	"os"

	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

// jsonCapableCommands lists the full command paths that honor --json.
// Any other runnable command rejects --json instead of silently ignoring it.
var jsonCapableCommands = map[string]bool{
	"paq ls":            true,
	"paq registry list": true,
	"paq registry show": true,
	"paq info":          true,
	"paq config show":   true,
	"paq import":        true,
	"paq search":        true,
	"paq outdated":      true,
}

var (
	flagNoColor bool
	flagJSON    bool
	flagQuiet   bool
	flagVerbose bool
	flagDebug   bool
)

var rootCmd = &cobra.Command{
	Use:   "paq",
	Short: "paq — install CLI tools from GitHub releases and URLs",
	Long:  `paq installs and manages CLI tools defined in a registry, downloading from GitHub releases or direct URLs.`,
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
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
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
}
