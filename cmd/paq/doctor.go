package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fmt"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/pathenv"
	"github.com/enr/paq/internal/platform"
	"github.com/enr/paq/internal/registry"
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

var doctorFix bool

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "add the bin dir to the user PATH if missing (Windows only)")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
	// problems counts only what stops paq from working — a manifest it cannot
	// parse, a path it cannot resolve — and makes doctor exit non-zero so it
	// can be used as a health check. Everything paq recovers from on its own
	// stays a warning with an exit code of 0: a corrupt registry cache (it
	// falls back to the embedded recipes), a missing manifest or state file,
	// a bin dir outside PATH, an unset GITHUB_TOKEN.
	problems := 0

	plat := platform.Detect()
	ui.OKField("Platform", plat.OS+"/"+plat.Arch)

	cfgPath, pathErr := config.UserManifestPath()
	switch {
	case pathErr != nil:
		ui.WarnField("Config", "path unknown", "("+pathErr.Error()+")")
		problems++
	default:
		if _, statErr := os.Stat(cfgPath); statErr != nil {
			ui.WarnField("Config", cfgPath, "(not found)")
		} else if _, parseErr := config.LoadUserConfig(); parseErr != nil {
			// Stat alone would report a green row for a manifest paq cannot
			// parse — the one case doctor most needs to surface.
			ui.WarnField("Config", cfgPath, "(unusable)")
			ui.Hint("%v", parseErr)
			problems++
		} else {
			ui.OKField("Config", cfgPath)
		}
	}

	if stPath, err := state.StatePath(); err != nil {
		ui.WarnField("State", "path unknown", "("+err.Error()+")")
		problems++
	} else if _, err := os.Stat(stPath); err == nil {
		ui.OKField("State", stPath)
	} else {
		ui.WarnField("State", stPath, "(not found — no apps installed yet)")
	}

	if _, meta, rerr := registry.Open(); rerr != nil {
		// Not counted as a problem: paq falls back to the embedded registry,
		// so this is degraded, not broken (see TestOfflineDegradation).
		ui.WarnField("Registry", "external cache unusable", "("+rerr.Error()+")")
		ui.Hint("run `paq registry update` to refresh the external registry")
	} else if meta != nil {
		value := fmt.Sprintf("external %s, %d recipes, fetched %s", meta.Version, meta.SpecCount, humanAge(meta.FetchedAt))
		if registryIsStale(meta) {
			ui.WarnField("Registry", value, fmt.Sprintf("(stale: older than paq %s)", Version))
			ui.Hint("run `paq registry update` to refresh the external registry")
		} else {
			ui.OKField("Registry", value)
		}
	} else {
		ui.OKField("Registry", "embedded only")
	}

	cfg, err := loadConfig()
	if err != nil {
		// Reported, not skipped: silently dropping the install-dir and PATH
		// rows leaves a report that looks healthy apart from a few absent lines.
		ui.WarnField("Install dirs", "unknown", "(configuration unusable)")
		ui.Hint("%v", err)
		problems++
	} else {
		binDir, optDir := config.DefaultDestRoots(cfg.Defaults)
		ui.OKField("Bin dir", binDir)
		ui.OKField("Opt dir", optDir)

		// Check whether bin dir is in PATH.
		resolvedBin, err := expandHome(binDir)
		if err != nil {
			return fmt.Errorf("resolve bin dir: %w", err)
		}
		inPath := false
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			if dir == resolvedBin {
				inPath = true
				break
			}
		}
		if inPath {
			ui.OK("Bin dir is in PATH")
		} else if doctorFix {
			added, err := pathenv.AddToUserPath(resolvedBin)
			if err != nil {
				return err
			}
			if added {
				ui.OK("Added %s to the user PATH", resolvedBin)
			} else {
				ui.OK("Bin dir %s is already in the user PATH", resolvedBin)
			}
			ui.Hint("restart your terminal to pick up the new PATH")
		} else {
			ui.Warn("Bin dir %s is NOT in PATH", resolvedBin)
			if runtime.GOOS == "windows" {
				ui.Hint("run `paq doctor --fix` to add it to your user PATH")
			} else {
				ui.Hint("add `export PATH=\"%s:$PATH\"` to your shell profile", resolvedBin)
			}
		}
	}

	if os.Getenv("GITHUB_TOKEN") != "" {
		ui.OKField("GITHUB_TOKEN", "set")
	} else {
		ui.WarnField("GITHUB_TOKEN", "not set", "(GitHub API calls may be rate-limited)")
		ui.Hint("set GITHUB_TOKEN to avoid rate-limiting when installing GitHub-backed tools")
	}

	if problems > 0 {
		return fmt.Errorf("%d problem(s) found", problems)
	}
	return nil
}

// expandHome expands a leading ~/ to the user's home directory, failing rather
// than returning the path unexpanded (a literal "~" would be reported as a
// real directory, and written into the user PATH by --fix).
// Duplicates the helper in internal/install to avoid a cross-package dependency.
func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot expand ~: %w", err)
	}
	return filepath.Join(home, path[2:]), nil
}
