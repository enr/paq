package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/version"
)

// TestRunUpgradeMultiArgFailsFastOnUnknownName verifies that when multiple
// app names are given, an unknown one makes the whole command fail before
// any upgrade is attempted, regardless of its position in the argument list.
func TestRunUpgradeMultiArgFailsFastOnUnknownName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	block := renderAppEntryTOML("rg", config.AppEntry{Use: "ripgrep", Version: "14.1.1"})
	if _, err := config.WriteManifestEntry("rg", block, false); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}

	if err := runUpgrade(upgradeCmd, []string{"rg", "typo-xyz-does-not-exist"}); err == nil {
		t.Error("expected an error when the second argument is unknown")
	}
	if err := runUpgrade(upgradeCmd, []string{"typo-xyz-does-not-exist", "rg"}); err == nil {
		t.Error("expected an error when the first argument is unknown")
	}
}

// TestRunUpgradeMultiArgPinnedSkipsWithoutError verifies that upgrade accepts
// several app names pinned to a fixed version: each is skipped (no network
// call, since they're not "latest") and the command succeeds.
func TestRunUpgradeMultiArgPinnedSkipsWithoutError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for name, use := range map[string]string{"rg": "ripgrep", "bat": "bat"} {
		block := renderAppEntryTOML(name, config.AppEntry{Use: use, Version: "1.0.0"})
		if _, err := config.WriteManifestEntry(name, block, false); err != nil {
			t.Fatalf("write manifest entry for %s: %v", name, err)
		}
	}

	// cobra only sets Command.Context() during Execute(); set it explicitly
	// since this test calls runUpgrade directly and it reaches the
	// errgroup.WithContext call for these (valid, pinned) apps.
	upgradeCmd.SetContext(context.Background())
	t.Cleanup(func() { upgradeCmd.SetContext(nil) })

	if err := runUpgrade(upgradeCmd, []string{"rg", "bat"}); err != nil {
		t.Errorf("expected pinned apps to be skipped without error, got: %v", err)
	}
}

// TestUpgradeAppEmptyVersionNoDefaultTracksLatest verifies that an app with
// NO version whose spec has NO default_version is treated as tracking latest
// (not skipped as "pinned"), matching AppEntry.TracksLatest and the pipeline's
// own version-resolution switch. The backend ("url", no latest_strategy)
// can't actually resolve "latest", so it reaches the "no upstream strategy"
// skip instead of the (buggy) "pinned to , skipping" path.
func TestUpgradeAppEmptyVersionNoDefaultTracksLatest(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	cfg := &config.Config{
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool"}, // no version
		},
		Specs: map[string]config.Spec{
			"tool": {Backend: "url", Source: "https://example.invalid/{{version}}.tar.gz"},
		},
	}
	if err := state.Update(func(st *state.State) error {
		st.Set(state.InstalledApp{Name: "tool", Version: "1.0.0"})
		return nil
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	var steps []string
	hooks := &install.Hooks{OnStep: func(msg string) { steps = append(steps, msg) }}

	if err := upgradeApp(context.Background(), cfg, "tool", hooks, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range steps {
		if strings.Contains(s, "pinned to") {
			t.Errorf("unexpected pinned skip message: %q", s)
		}
	}
	if len(steps) == 0 || !strings.Contains(steps[len(steps)-1], "no upstream version to resolve") {
		t.Fatalf("expected the \"no upstream strategy\" skip message, got %v", steps)
	}
}

// TestUpgradeAppEmptyVersionWithDefaultSkipsWithDefaultInMessage verifies
// that an app with NO version whose spec HAS a default_version is skipped as
// pinned, and the skip message names the default version rather than
// printing an empty string ("pinned to , skipping").
func TestUpgradeAppEmptyVersionWithDefaultSkipsWithDefaultInMessage(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	cfg := &config.Config{
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool"}, // no version
		},
		Specs: map[string]config.Spec{
			"tool": {Backend: "url", Source: "https://example.invalid/{{version}}.tar.gz", DefaultVersion: "2.0.0"},
		},
	}

	var steps []string
	hooks := &install.Hooks{OnStep: func(msg string) { steps = append(steps, msg) }}

	if err := upgradeApp(context.Background(), cfg, "tool", hooks, nil); err != nil {
		t.Fatalf("expected pinned app to be skipped without error, got: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected exactly one step message, got %v", steps)
	}
	if !strings.Contains(steps[0], "2.0.0") {
		t.Errorf("skip message = %q, want it to mention the default version 2.0.0", steps[0])
	}
	if strings.Contains(steps[0], "pinned to , ") {
		t.Errorf("skip message = %q, must not print an empty version", steps[0])
	}
}

// TestResolveLatestVersionWarnsOnUnsupportedBackendWithExplicitAge verifies
// that an explicit minimum_release_age (spec- or defaults-level) on a backend
// that can't honor it (anything but plain "github") produces exactly one
// warning and does not otherwise change the outcome: resolution still fails
// the normal way (ErrLatestNotImplemented for backend "url" with no
// latest_strategy), with no network call needed.
func TestResolveLatestVersionWarnsOnUnsupportedBackendWithExplicitAge(t *testing.T) {
	cases := []struct {
		name string
		spec config.Spec
		cfg  config.Config
	}{
		{
			name: "spec-level override",
			spec: config.Spec{Backend: "url", Source: "https://example.invalid/{{version}}.tar.gz", MinimumReleaseAge: "7d"},
		},
		{
			name: "global default",
			spec: config.Spec{Backend: "url", Source: "https://example.invalid/{{version}}.tar.gz"},
			cfg:  config.Config{Defaults: config.Defaults{MinimumReleaseAge: "7d"}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var warnings []string
			warn := func(format string, a ...any) { warnings = append(warnings, format) }

			_, err := resolveLatestVersion(context.Background(), &c.cfg, c.spec, warn)
			if !errors.Is(err, version.ErrLatestNotImplemented) {
				t.Fatalf("expected ErrLatestNotImplemented, got %v", err)
			}
			if len(warnings) != 1 {
				t.Fatalf("expected exactly one warning, got %v", warnings)
			}
			if !strings.Contains(warnings[0], "minimum_release_age") {
				t.Errorf("warning = %q, want it to mention minimum_release_age", warnings[0])
			}
		})
	}
}

// TestResolveLatestVersionWarnsForArchLinuxStrategy verifies that the
// "unsupported backend" warning also fires for the arch-linux strategy (it
// has no per-release publish dates either), independently of the backend
// field. ArchPkg is left empty so ArchLinuxProvider fails fast on its own
// validation, with no network call.
func TestResolveLatestVersionWarnsForArchLinuxStrategy(t *testing.T) {
	spec := config.Spec{Backend: "url", LatestStrategy: "arch-linux", MinimumReleaseAge: "7d"}
	cfg := config.Config{}

	var warnings []string
	warn := func(format string, a ...any) { warnings = append(warnings, format) }

	_, err := resolveLatestVersion(context.Background(), &cfg, spec, warn)
	if err == nil {
		t.Fatal("expected an error (empty arch_pkg), got nil")
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "minimum_release_age") {
		t.Errorf("warning = %q, want it to mention minimum_release_age", warnings[0])
	}
}

// TestResolveLatestVersionNoWarnWithoutExplicitAge verifies that the
// (invisible) built-in default minimum age never triggers the "not
// supported" warning on a backend that can't honor it - only an explicit
// spec/defaults value does.
func TestResolveLatestVersionNoWarnWithoutExplicitAge(t *testing.T) {
	spec := config.Spec{Backend: "url", Source: "https://example.invalid/{{version}}.tar.gz"}
	cfg := config.Config{}

	var warnings []string
	warn := func(format string, a ...any) { warnings = append(warnings, format) }

	_, err := resolveLatestVersion(context.Background(), &cfg, spec, warn)
	if !errors.Is(err, version.ErrLatestNotImplemented) {
		t.Fatalf("expected ErrLatestNotImplemented, got %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for the implicit default, got %v", warnings)
	}
}

// TestResolveLatestVersionInvalidMinimumAge verifies that a malformed
// minimum_release_age fails fast with a clear error, without attempting to
// resolve "latest" at all.
func TestResolveLatestVersionInvalidMinimumAge(t *testing.T) {
	spec := config.Spec{Backend: "github", Repo: "test/repo", MinimumReleaseAge: "not-a-duration"}
	cfg := config.Config{}

	_, err := resolveLatestVersion(context.Background(), &cfg, spec, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid minimum_release_age") {
		t.Fatalf("expected an invalid minimum_release_age error, got %v", err)
	}
}
