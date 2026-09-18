package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

// doctorCheck is one row of the report, in the shape "paq doctor --json"
// emits it. Human output renders the same data through ui.OKField/WarnField;
// see the report/reportOK/reportWarn helpers below.
type doctorCheck struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"` // "ok" or "warn"
	Value   string   `json:"value,omitempty"`
	Note    string   `json:"note,omitempty"`
	Hint    string   `json:"hint,omitempty"`
	Details []string `json:"details,omitempty"`
	Problem bool     `json:"problem,omitempty"`
}

// doctorReport is the top-level JSON document for "paq doctor --json":
// problems mirrors the count that also drives the command's exit code, so a
// script can gate on it without counting checks itself.
type doctorReport struct {
	Checks   []doctorCheck `json:"checks"`
	Problems int           `json:"problems"`
}

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
	// problems counts what paq cannot put right on its own and the user must
	// act on — a manifest it cannot parse, a path it cannot resolve, a state
	// record whose files are gone — and makes doctor exit non-zero so it can be
	// used as a health check.
	//
	// Anything paq handles by itself stays a warning at exit 0: a corrupt
	// registry cache (it falls back to the embedded recipes), a manifest or
	// state file that does not exist yet, a bin dir outside PATH, an unset
	// GITHUB_TOKEN.
	var checks []doctorCheck
	problems := 0

	// reportOK/reportWarn record one row of the "label: value" shape: in
	// --json mode into checks (jsonName), otherwise through the same
	// ui.OKField/WarnField calls the human report always used (humanLabel).
	// Kept side by side (rather than one rendering pass over the collected
	// checks) so the human output is byte-for-byte what it was before --json
	// existed.
	reportOK := func(jsonName, humanLabel, value string) {
		if ui.Global.JSON {
			checks = append(checks, doctorCheck{Name: jsonName, Status: "ok", Value: value})
			return
		}
		ui.OKField(humanLabel, value)
	}
	reportWarn := func(jsonName, humanLabel, value, note, hint string, isProblem bool, details ...string) {
		if isProblem {
			problems++
		}
		if ui.Global.JSON {
			checks = append(checks, doctorCheck{
				Name: jsonName, Status: "warn", Value: value, Note: note, Hint: hint,
				Details: details, Problem: isProblem,
			})
			return
		}
		note2 := ""
		if note != "" {
			note2 = "(" + note + ")"
		}
		ui.WarnField(humanLabel, value, note2)
		for _, d := range details {
			ui.Warn("  %s", d)
		}
		if hint != "" {
			ui.Hint("%s", hint)
		}
	}

	plat := platform.Detect()
	reportOK("platform", "Platform", plat.OS+"/"+plat.Arch)

	cfgPath, pathErr := config.UserManifestPath()
	switch {
	case pathErr != nil:
		reportWarn("config", "Config", "path unknown", pathErr.Error(), "", true)
	default:
		if _, statErr := os.Stat(cfgPath); statErr != nil {
			reportWarn("config", "Config", cfgPath, "not found", "", false)
		} else if _, parseErr := config.LoadUserConfig(); parseErr != nil {
			// Stat alone would report a green row for a manifest paq cannot
			// parse — the one case doctor most needs to surface.
			reportWarn("config", "Config", cfgPath, "unusable", parseErr.Error(), true)
		} else {
			reportOK("config", "Config", cfgPath)
		}
	}

	if stPath, err := state.StatePath(); err != nil {
		reportWarn("state", "State", "path unknown", err.Error(), "", true)
	} else if _, err := os.Stat(stPath); err == nil {
		reportOK("state", "State", stPath)
	} else {
		reportWarn("state", "State", stPath, "not found — no apps installed yet", "", false)
	}

	// Reconcile the state DB against the filesystem. Counted as a problem:
	// unlike a corrupt registry cache, paq cannot recover from this on its own
	// — ls, which, upgrade and uninstall all trust the record — and until the
	// user acts, the state DB claims something that is not true.
	if st, stErr := state.Load(); stErr != nil {
		reportWarn("installed", "Installed", "unknown", "state DB unreadable", stErr.Error(), true)
	} else if len(st.Packages) > 0 {
		var drifted []string
		for _, rec := range st.Packages {
			if gone := rec.MissingPaths(); len(gone) > 0 {
				drifted = append(drifted, fmt.Sprintf("%s@%s → %s", rec.Name, rec.Version, strings.Join(gone, ", ")))
			}
		}
		if len(drifted) > 0 {
			sort.Strings(drifted)
			reportWarn("installed", "Installed",
				fmt.Sprintf("%d tool(s)", len(st.Packages)),
				fmt.Sprintf("%d missing on disk", len(drifted)),
				"reinstall them with `paq install <name>`, or drop the record with `paq uninstall <name>`",
				true, drifted...)
		} else {
			reportOK("installed", "Installed", fmt.Sprintf("%d tool(s), all present on disk", len(st.Packages)))
		}
	}

	if _, meta, rerr := registry.Open(); rerr != nil {
		// Not counted as a problem: paq falls back to the embedded registry,
		// so this is degraded, not broken (see TestOfflineDegradation).
		reportWarn("registry", "Registry", "external cache unusable", rerr.Error(),
			"run `paq registry update` to refresh the external registry", false)
	} else if meta != nil {
		value := fmt.Sprintf("external %s, %d recipes, fetched %s", meta.Version, meta.SpecCount, humanAge(meta.FetchedAt))
		if registryIsStale(meta) {
			reportWarn("registry", "Registry", value, fmt.Sprintf("stale: older than paq %s", Version),
				"run `paq registry update` to refresh the external registry", false)
		} else {
			reportOK("registry", "Registry", value)
		}
	} else {
		reportOK("registry", "Registry", "embedded only")
	}

	cfg, err := loadConfig()
	if err != nil {
		// Reported, not skipped: silently dropping the install-dir and PATH
		// rows leaves a report that looks healthy apart from a few absent lines.
		reportWarn("install_dirs", "Install dirs", "unknown", "configuration unusable", err.Error(), true)
	} else {
		binDir, optDir := config.DefaultDestRoots(cfg.Defaults)
		reportOK("bin_dir", "Bin dir", binDir)
		reportOK("opt_dir", "Opt dir", optDir)

		// Check whether bin dir is in PATH. Not a "label: value" row in the
		// human report (a plain OK/Warn line instead), so it bypasses
		// reportOK/reportWarn to keep that wording exactly as before --json.
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
		switch {
		case inPath:
			if ui.Global.JSON {
				checks = append(checks, doctorCheck{Name: "path", Status: "ok", Value: "bin dir is in PATH"})
			} else {
				ui.OK("Bin dir is in PATH")
			}
		case doctorFix:
			added, err := pathenv.AddToUserPath(resolvedBin)
			if err != nil {
				return err
			}
			if ui.Global.JSON {
				value := fmt.Sprintf("bin dir %s is already in the user PATH", resolvedBin)
				if added {
					value = fmt.Sprintf("added %s to the user PATH", resolvedBin)
				}
				checks = append(checks, doctorCheck{Name: "path", Status: "ok", Value: value})
			} else {
				if added {
					ui.OK("Added %s to the user PATH", resolvedBin)
				} else {
					ui.OK("Bin dir %s is already in the user PATH", resolvedBin)
				}
				ui.Hint("restart your terminal to pick up the new PATH")
			}
		default:
			hint := fmt.Sprintf("add `export PATH=\"%s:$PATH\"` to your shell profile", resolvedBin)
			if runtime.GOOS == "windows" {
				hint = "run `paq doctor --fix` to add it to your user PATH"
			}
			// Advisory only: an app installed via `paq install` still runs by
			// full path, so a bin dir outside PATH is not counted as a problem.
			if ui.Global.JSON {
				checks = append(checks, doctorCheck{
					Name: "path", Status: "warn",
					Value: fmt.Sprintf("bin dir %s is NOT in PATH", resolvedBin), Hint: hint,
				})
			} else {
				ui.Warn("Bin dir %s is NOT in PATH", resolvedBin)
				ui.Hint("%s", hint)
			}
		}
	}

	if os.Getenv("GITHUB_TOKEN") != "" {
		reportOK("github_token", "GITHUB_TOKEN", "set")
	} else {
		reportWarn("github_token", "GITHUB_TOKEN", "not set", "GitHub API calls may be rate-limited",
			"set GITHUB_TOKEN to avoid rate-limiting when installing GitHub-backed tools", false)
	}

	if ui.Global.JSON {
		data, _ := json.MarshalIndent(doctorReport{Checks: checks, Problems: problems}, "", "  ")
		fmt.Println(string(data))
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
