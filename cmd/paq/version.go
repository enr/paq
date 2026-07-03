package main

import (
	"fmt"
	"runtime"

	"github.com/enr/paq/internal/registry"
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
	return fmt.Sprintf("paq %s\n  revision:  %s\n  buildtime: %s\n  go:        %s\n  registry:  %s",
		Version, Revision, BuildTime, runtime.Version(), registryVersionLine())
}

// registryVersionLine describes the active registry for "paq version":
// the external snapshot version when installed, otherwise the embedded one.
func registryVersionLine() string {
	if _, meta, err := registry.Open(); err == nil && meta != nil {
		return fmt.Sprintf("%s (external)", meta.Version)
	}
	return "embedded"
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
