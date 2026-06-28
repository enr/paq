package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:     "uninstall <app[@version]>",
	Aliases: []string{"rm", "remove"},
	Short:   "Uninstall a tool (use app@version to disambiguate multiple versions)",
	Args:    cobra.ExactArgs(1),
	RunE:    runUninstall,
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	name, version := parseAppRef(args[0])

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	matches := st.ByName(name)
	if len(matches) == 0 {
		ui.Fail("%q is not installed", name)
		ui.Hint("list installed tools with `paq ls`")
		os.Exit(1)
	}

	// Seleziona le entry da rimuovere
	var targets []state.InstalledApp
	if version != "" {
		rec, ok := st.Get(name, version)
		if !ok {
			ui.Fail("%s@%s is not installed", name, version)
			ui.Hint("list installed versions with `paq ls`")
			os.Exit(1)
		}
		targets = []state.InstalledApp{rec}
	} else if len(matches) == 1 {
		targets = matches
	} else {
		// Più versioni installate: richiedi disambiguazione
		var versions []string
		for _, m := range matches {
			versions = append(versions, m.Version)
		}
		ui.Fail("multiple versions of %q installed: %s", name, strings.Join(versions, ", "))
		ui.Hint("specify one with %s@<version>", name)
		os.Exit(1)
	}

	for _, rec := range targets {
		fmt.Printf("Uninstalling %s %s from %s...\n", rec.Name, rec.Version, rec.Dest)
		if err := removeRecordFiles(rec); err != nil {
			return err
		}
		st.Delete(rec.Name, rec.Version)
	}

	if err := st.Save(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	fmt.Printf("✓ %s uninstalled\n", name)
	return nil
}

// removeRecordFiles rimuove dal filesystem i file o le directory installati
// per una entry di stato, in base al suo Kind.
func removeRecordFiles(rec state.InstalledApp) error {
	switch rec.Kind {
	case "file":
		if err := os.Remove(rec.Dest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", rec.Dest, err)
		}
	case "dir":
		if err := os.RemoveAll(rec.Dest); err != nil {
			return fmt.Errorf("remove %s: %w", rec.Dest, err)
		}
	case "binaries":
		// Rimuovi solo i file installati, non la bin dir condivisa (es. ~/.local/bin).
		for _, p := range rec.Files {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", p, err)
			}
		}
	default:
		return fmt.Errorf("unknown kind %q for %s", rec.Kind, rec.Name)
	}
	return nil
}

// parseAppRef separa un riferimento "name" o "name@version".
func parseAppRef(ref string) (name, version string) {
	if i := strings.LastIndex(ref, "@"); i > 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}
