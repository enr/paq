package main

import (
	"context"
	"time"

	"github.com/enr/paq/internal/updatecheck"
	"github.com/enr/paq/internal/version"
	"github.com/spf13/cobra"
)

// bgUpdateCheckTimeout bounds the release lookup performed by the detached
// background check.
const bgUpdateCheckTimeout = 10 * time.Second

// updateCheckCmd is the hidden command spawned in the background by the update
// notifier. It resolves the latest paq release and records it in the
// update-check cache for the next foreground command to display. It is silent
// and never reports an error to the user.
var updateCheckCmd = &cobra.Command{
	Use:    "__update-check",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), bgUpdateCheckTimeout)
		defer cancel()

		latest, tag, err := version.GitHubReleaseProvider{Repo: selfUpdateRepo}.Resolve(ctx)
		if err != nil {
			// Stay silent: the parent already bumped LastChecked, so the
			// 24h back-off holds even when the lookup fails (e.g. offline).
			return nil
		}

		st, _ := updatecheck.Load()
		st.LastChecked = time.Now()
		st.LatestVersion = latest
		st.LatestTag = tag
		_ = st.Save()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCheckCmd)
}
