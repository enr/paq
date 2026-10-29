package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/nodevuln"
	"github.com/enr/paq/internal/osv"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check installed tools for known vulnerabilities",
	Long: "Look up every installed tool version in a vulnerability database (OSV at osv.dev, " +
		"or the Node.js Security WG feed for Node.js) and list the known vulnerabilities " +
		"affecting it. Only tools whose recipe names a source (an `audit` table, or the npm " +
		"backend) can be checked; the others are shown with \"?\". Exits with code 3 when at " +
		"least one installed version is vulnerable.",
	Args: cobra.NoArgs,
	RunE: runAudit,
}

// errVulnerable marks the audit outcome "some installed version is
// vulnerable", mapped to its own exit code.
var errVulnerable = errors.New("known vulnerabilities found")

func init() {
	rootCmd.AddCommand(auditCmd)
}

func runAudit(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	if len(st.Packages) == 0 && !ui.Global.JSON {
		fmt.Println("No tools installed.")
		return nil
	}

	entries, targets := auditQueries(cfg, st)
	for source, ts := range targets {
		ids, err := auditSources[source](cmd.Context(), ts)
		if err != nil {
			return err
		}
		for i, t := range ts {
			entries[t.row].Vulns = ids[i]
		}
	}
	vulnerable := 0
	for _, e := range entries {
		if len(e.Vulns) > 0 {
			vulnerable++
		}
	}

	if err := ui.PrintAuditTable(entries); err != nil {
		return err
	}
	if vulnerable > 0 {
		return fmt.Errorf("%w: %d of %d installed tools", errVulnerable, vulnerable, len(entries))
	}
	return nil
}

// auditTarget is an installed version to look up in a vulnerability source,
// for row of the audit table.
type auditTarget struct {
	src     config.AuditSource
	version string
	row     int
}

// auditSources maps each audit source name to its lookup, which returns the
// vulnerability IDs of every target, in order, with a single query.
var auditSources = map[string]func(ctx context.Context, targets []auditTarget) ([][]string, error){
	config.AuditSourceOSV: func(ctx context.Context, targets []auditTarget) ([][]string, error) {
		queries := make([]osv.Query, len(targets))
		for i, t := range targets {
			queries[i] = osv.Query{Ecosystem: t.src.Ecosystem, Package: t.src.Package, Version: t.version}
		}
		return osv.QueryBatch(ctx, nil, queries)
	},
	config.AuditSourceNodejs: func(ctx context.Context, targets []auditTarget) ([][]string, error) {
		versions := make([]string, len(targets))
		for i, t := range targets {
			versions[i] = t.version
		}
		return nodevuln.Check(ctx, nil, runtime.GOOS, versions)
	},
}

// auditQueries builds one table row per installed record and groups, by
// source, the records whose spec names a vulnerability source. A record whose
// spec is gone from the registry, or names a source this paq does not know
// (e.g. from a newer registry snapshot), is listed as unchecked rather than
// failing the whole audit.
func auditQueries(cfg *config.Config, st *state.State) (entries []ui.AuditEntry, targets map[string][]auditTarget) {
	entries = make([]ui.AuditEntry, len(st.Packages))
	targets = make(map[string][]auditTarget)
	for i, rec := range st.Packages {
		entries[i] = ui.AuditEntry{Name: rec.Name, Version: rec.Version, Vulns: []string{}}

		specName := cfg.Apps[rec.Name].Use
		if specName == "" {
			specName = rec.Name
		}
		src, ok := cfg.Specs[specName].AuditTarget()
		if !ok {
			continue
		}
		if _, known := auditSources[src.Source]; !known {
			ui.Warn("spec %q: unknown audit source %q, not checked (a newer paq may support it)", specName, src.Source)
			continue
		}
		if src.Source == config.AuditSourceOSV && (src.Ecosystem == "" || src.Package == "") {
			ui.Warn("spec %q: audit source \"osv\" needs ecosystem and package, not checked", specName)
			continue
		}
		entries[i].Checked = true
		targets[src.Source] = append(targets[src.Source], auditTarget{src: src, version: rec.Version, row: i})
	}
	return entries, targets
}
