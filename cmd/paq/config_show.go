package main

import (
	"os"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the evaluated configuration path and its data",
	Long:  "Show the path of the user configuration file, the effective default install directories, and the apps it declares.",
	Args:  cobra.NoArgs,
	RunE:  runConfigShow,
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	path, err := config.UserManifestPath()
	if err != nil {
		return err
	}
	exists := false
	if _, statErr := os.Stat(path); statErr == nil {
		exists = true
	}

	effBin, effOpt := config.DefaultDestRoots(cfg.Defaults)
	ui.PrintConfigShow(path, exists, cfg.Defaults, effBin, effOpt, cfg.Apps, cfg.Registry)
	return nil
}
