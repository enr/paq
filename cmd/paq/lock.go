package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/ui"
)

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Manage paq.lock.toml",
	Long:  "Manage paq.lock.toml, the lockfile next to the manifest that pins the version (and each platform's archive sha256) of apps tracking \"latest\".",
	// With no subcommand, show the help.
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var lockDeleteCmd = &cobra.Command{
	Use:   "delete <app>...",
	Short: "Remove apps' entries from paq.lock.toml",
	Long: `Remove apps' entries from paq.lock.toml, so the next ` + "`paq install`" + ` resolves
"latest" again and records a fresh entry. Use it when upstream replaced a
release's archive and the install fails against the locked sha256.`,
	Example: `  paq lock delete rg
  paq lock delete rg bat`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeLockedApps,
	RunE:              runLockDelete,
}

func init() {
	lockCmd.AddCommand(lockDeleteCmd)
	rootCmd.AddCommand(lockCmd)
}

func runLockDelete(cmd *cobra.Command, args []string) error {
	var missing []string
	for _, name := range args {
		deleted, err := config.DeleteLockEntry(name)
		if err != nil {
			return err
		}
		if !deleted {
			missing = append(missing, name)
			continue
		}
		ui.OK("%s: lock entry removed", name)
	}
	if len(missing) > 0 {
		return hintError{
			msg:  fmt.Sprintf("no lock entry for %s", strings.Join(missing, ", ")),
			hint: "only apps tracking \"latest\" get one; `paq info <app>` shows an app's locked version",
		}
	}
	return nil
}
