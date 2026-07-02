package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Variables populated at build time via ldflags (see .sdlc/build).
var (
	Version   = "dev"
	Revision  = "unknown"
	BuildTime = "unknown"
)

// versionInfo formats the version and build metadata for "paq version" and "paq --version".
func versionInfo() string {
	return fmt.Sprintf("paq %s\n  revision:  %s\n  buildtime: %s\n  go:        %s",
		Version, Revision, BuildTime, runtime.Version())
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of paq",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(versionInfo())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.Version = versionInfo()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}
