package main

import (
	"fmt"
	"runtime"

	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/version"
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
	_, meta, err := registry.Open()
	if err != nil || meta == nil {
		return "embedded"
	}
	if registryIsStale(meta) {
		return fmt.Sprintf("%s (external, stale)", meta.Version)
	}
	return fmt.Sprintf("%s (external)", meta.Version)
}

// registryIsStale reports whether the external registry snapshot predates the
// running binary. Registry and paq are versioned and released together, so an
// older snapshot keeps overlaying the recipes embedded in the binary with its
// own stale definitions until `paq registry update` runs.
// A snapshot from a custom source has its own version line, and an unversioned
// build ("dev") compares as 0.0.0: neither is ever reported stale.
func registryIsStale(meta *registry.Meta) bool {
	if meta == nil || meta.Tag == "custom" {
		return false
	}
	return version.Compare(version.Clean(meta.Version), version.Clean(Version)) < 0
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
