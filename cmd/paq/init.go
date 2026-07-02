package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var flagInitForce bool

// initManifestSkeleton is the commented starter manifest written by `paq init`.
// Everything is commented out so the resulting file is valid, empty TOML:
// paq runs fine against it and falls back to its built-in defaults.
const initManifestSkeleton = `# paq user manifest.
#
# Uncomment and edit [defaults] to change where tools are installed.
# Built-in defaults: ~/.local/bin (binaries), ~/.local/opt (directories),
# or their Windows equivalents under %LOCALAPPDATA%\paq.
#
# [defaults]
# bin = "~/.local/bin"
# opt = "~/.local/opt"

# Add a tool with 'paq import <name> --write', or add an [apps.<name>]
# table by hand, e.g.:
#
# [apps.rg]
# use = "ripgrep"
# version = "latest"
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a commented manifest skeleton at the default config path",
	Long: "Create the user manifest (~/.config/paq/config.toml or its platform equivalent) " +
		"with a commented skeleton, so a new install has something to edit instead of a blank file.",
	Example: `  paq init
  paq init --force   # overwrite an existing manifest`,
	Args: cobra.NoArgs,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVarP(&flagInitForce, "force", "f", false, "Overwrite an existing manifest")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	path, err := config.UserManifestPath()
	if err != nil {
		return fmt.Errorf("resolve manifest path: %w", err)
	}

	if !flagInitForce {
		if _, statErr := os.Stat(path); statErr == nil {
			return hintError{
				msg:  fmt.Sprintf("manifest already exists at %s", path),
				hint: "use --force to overwrite it",
			}
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("stat manifest: %w", statErr)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Atomic write: write to temp then rename, consistent with how the rest
	// of the manifest-writing code (WriteManifestEntry) touches this file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(initManifestSkeleton), 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace manifest: %w", err)
	}

	ui.OK("created %s", path)
	ui.Hint("search for a tool with `paq search <name>`, then install it with `paq install <name>`")
	return nil
}
