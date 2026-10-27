package main

import (
	"errors"
	"fmt"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/osv"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check installed tools for known vulnerabilities",
	Long: "Look up every installed tool version in the OSV vulnerability database (osv.dev) " +
		"and list the known vulnerabilities affecting it. Only tools whose recipe maps to an " +
		"OSV package (an `osv` table, or the npm backend) can be checked; the others are shown " +
		"with \"?\". Exits with code 3 when at least one installed version is vulnerable.",
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

	entries, queries, queryRow := auditQueries(cfg, st)
	ids, err := osv.QueryBatch(cmd.Context(), nil, queries)
	if err != nil {
		return err
	}
	vulnerable := 0
	for i, row := range queryRow {
		entries[row].Vulns = ids[i]
		if len(ids[i]) > 0 {
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

// auditQueries builds one table row per installed record and an OSV query
// for each record whose spec has an OSV mapping; queryRow[i] is the row
// queries[i] belongs to. A record whose spec is gone from the registry is
// listed as unchecked rather than failing the whole audit.
func auditQueries(cfg *config.Config, st *state.State) (entries []ui.AuditEntry, queries []osv.Query, queryRow []int) {
	entries = make([]ui.AuditEntry, len(st.Packages))
	for i, rec := range st.Packages {
		entries[i] = ui.AuditEntry{Name: rec.Name, Version: rec.Version, Vulns: []string{}}

		specName := cfg.Apps[rec.Name].Use
		if specName == "" {
			specName = rec.Name
		}
		pkg, ok := cfg.Specs[specName].OSVTarget()
		if !ok {
			continue
		}
		entries[i].Checked = true
		queries = append(queries, osv.Query{Ecosystem: pkg.Ecosystem, Package: pkg.Package, Version: rec.Version})
		queryRow = append(queryRow, i)
	}
	return entries, queries, queryRow
}
