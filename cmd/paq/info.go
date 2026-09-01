package main

import (
	"fmt"
	"strings"

	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/platform"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/version"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:               "info <app>",
	Short:             "Show spec and install state for a tool",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeManifestApps,
	RunE:              runInfo,
}

func init() {
	rootCmd.AddCommand(infoCmd)
}

func runInfo(cmd *cobra.Command, args []string) error {
	appName := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	app, ok := cfg.Apps[appName]
	if !ok {
		return hintError{
			msg:  fmt.Sprintf("app %q not found in manifest (~/.config/paq/config.toml)", appName),
			hint: fmt.Sprintf("list configured apps with `paq ls`, or add it under [apps.%s] in your manifest", appName),
		}
	}

	specName := app.Use
	if specName == "" {
		specName = appName
	}
	spec, ok := cfg.Specs[specName]
	if !ok {
		return fmt.Errorf("spec %q not found in registry", specName)
	}

	// Load the installed versions (there may be more than one).
	var installed []state.InstalledApp
	if st, err := state.Load(); err == nil {
		installed = st.ByName(appName)
	}

	// Resolve the placeholders (e.g. {{rust_target}}) purely offline: platform
	// detection, arch/os/env overrides and meta-templates don't need network
	// access. The version is only filled in when it's pinned (explicitly or via
	// default_version); "latest" would require a network call, which `info`
	// doesn't make, so {{version}}-derived placeholders are left unresolved.
	plat := platform.Detect()
	resolvedSpec, vars, _ := install.ResolveVars(cfg, plat, spec, app)
	spec = resolvedSpec
	if app.Version != "" && !strings.EqualFold(app.Version, "latest") {
		vars.Version = app.Version
	} else if spec.DefaultVersion != "" {
		vars.Version = spec.DefaultVersion
	}
	if vars.Version != "" {
		vars.VersionMajor, vars.VersionMinor, vars.VersionPatch = version.Parse(vars.Version)
	}

	ui.PrintInfoDetail(appName, spec, app, installed, vars)
	return nil
}
