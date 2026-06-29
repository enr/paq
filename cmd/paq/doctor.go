package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/platform"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check paq environment and configuration",
	Long:  "Print diagnostic information about paq's environment: platform, config/state paths, install directories, and PATH.",
	Args:  cobra.NoArgs,
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
	plat := platform.Detect()
	ui.OKField("Platform", plat.OS+"/"+plat.Arch)

	if cfgPath, err := config.UserManifestPath(); err == nil {
		if _, err := os.Stat(cfgPath); err == nil {
			ui.OKField("Config", cfgPath)
		} else {
			ui.WarnField("Config", cfgPath, "(not found)")
		}
	}

	if stPath, err := state.StatePath(); err == nil {
		if _, err := os.Stat(stPath); err == nil {
			ui.OKField("State", stPath)
		} else {
			ui.WarnField("State", stPath, "(not found — no apps installed yet)")
		}
	}

	cfg, err := loadConfig()
	if err == nil {
		binDir, optDir := config.DefaultDestRoots(cfg.Defaults)
		ui.OKField("Bin dir", binDir)
		ui.OKField("Opt dir", optDir)

		// Check whether bin dir is in PATH.
		resolvedBin := expandHome(binDir)
		inPath := false
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			if dir == resolvedBin {
				inPath = true
				break
			}
		}
		if inPath {
			ui.OK("Bin dir is in PATH")
		} else {
			ui.Warn("Bin dir %s is NOT in PATH", resolvedBin)
			ui.Hint("add `export PATH=\"%s:$PATH\"` to your shell profile", resolvedBin)
		}
	}

	if os.Getenv("GITHUB_TOKEN") != "" {
		ui.OKField("GITHUB_TOKEN", "set")
	} else {
		ui.WarnField("GITHUB_TOKEN", "not set", "(GitHub API calls may be rate-limited)")
		ui.Hint("set GITHUB_TOKEN to avoid rate-limiting when installing GitHub-backed tools")
	}

	return nil
}

// expandHome expands a leading ~/ to the user's home directory.
// Duplicates the helper in internal/install to avoid a cross-package dependency.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
