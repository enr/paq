package main

import (
	"os"

	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

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
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		ui.Global = ui.Config{
			NoColor: flagNoColor || os.Getenv("NO_COLOR") != "",
			JSON:    flagJSON,
			Quiet:   flagQuiet,
			// --debug implies --verbose output.
			Verbose: flagVerbose || flagDebug,
			Debug:   flagDebug,
		}
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
	rootCmd.PersistentFlags().BoolVarP(&flagDebug, "debug", "d", false, "Print detailed debug output to stderr (implies --verbose)")
}
