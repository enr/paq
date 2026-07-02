package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
)

// newTestConfig builds a minimal Config with a "ripgrep" spec in the
// registry and no app in the manifest.
func newTestConfig() *config.Config {
	return &config.Config{
		Specs: map[string]config.Spec{
			"ripgrep": {Extract: "rg{{ext}}"},
		},
		Apps: map[string]config.AppEntry{},
	}
}

func TestEnsureManifestEntryAutoImportsAndWrites(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := newTestConfig()
	path, err := ensureManifestEntry(cfg, "ripgrep", true)
	if err != nil {
		t.Fatalf("ensureManifestEntry: %v", err)
	}
	if path == "" {
		t.Fatal("expected a manifest path, got empty")
	}
	if _, ok := cfg.Apps["ripgrep"]; !ok {
		t.Fatal("expected cfg.Apps[ripgrep] to be set in memory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(data), "[apps.ripgrep]") {
		t.Fatalf("manifest missing [apps.ripgrep], got:\n%s", data)
	}
}

func TestEnsureManifestEntryNoSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := newTestConfig()
	path, err := ensureManifestEntry(cfg, "ripgrep", false)
	if err != nil {
		t.Fatalf("ensureManifestEntry: %v", err)
	}
	if path != "" {
		t.Fatalf("expected empty path with save=false, got %q", path)
	}
	if _, ok := cfg.Apps["ripgrep"]; !ok {
		t.Fatal("expected cfg.Apps[ripgrep] to be set in memory")
	}
	if _, err := os.Stat(filepath.Join(dir, "paq", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected no manifest file with save=false, stat err = %v", err)
	}
}

func TestEnsureManifestEntryExistingApp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := newTestConfig()
	cfg.Apps["ripgrep"] = config.AppEntry{Use: "ripgrep", Version: "1.2.3"}
	path, err := ensureManifestEntry(cfg, "ripgrep", true)
	if err != nil {
		t.Fatalf("ensureManifestEntry: %v", err)
	}
	if path != "" {
		t.Fatalf("expected empty path for existing app, got %q", path)
	}
	if cfg.Apps["ripgrep"].Version != "1.2.3" {
		t.Fatal("existing entry must not be overwritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "paq", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("existing app must not trigger a manifest write")
	}
}

func TestEnsureManifestEntryUnknownSpec(t *testing.T) {
	cfg := newTestConfig()
	_, err := ensureManifestEntry(cfg, "rip", true) // substring of "ripgrep"
	if err == nil {
		t.Fatal("expected an error for unknown spec")
	}
	var he hintError
	if !errors.As(err, &he) {
		t.Fatalf("expected hintError, got %T: %v", err, err)
	}
	if !strings.Contains(he.hint, "did you mean") || !strings.Contains(he.hint, "ripgrep") {
		t.Fatalf("expected a did-you-mean hint naming ripgrep, got %q", he.hint)
	}
}

func TestEnsureManifestEntryUnknownNoSuggestion(t *testing.T) {
	cfg := newTestConfig()
	_, err := ensureManifestEntry(cfg, "zzz", true) // no substring match
	var he hintError
	if !errors.As(err, &he) {
		t.Fatalf("expected hintError, got %T: %v", err, err)
	}
	if !strings.Contains(he.hint, "paq registry") {
		t.Fatalf("expected the registry fallback hint, got %q", he.hint)
	}
}

func TestValidateAppName(t *testing.T) {
	cfg := newTestConfig()
	cfg.Apps["existing"] = config.AppEntry{Use: "ripgrep"}

	if err := validateAppName(cfg, "existing"); err != nil {
		t.Errorf("existing manifest app should validate, got: %v", err)
	}
	if err := validateAppName(cfg, "ripgrep"); err != nil {
		t.Errorf("known registry spec should validate, got: %v", err)
	}
	if err := validateAppName(cfg, "typo-xyz"); err == nil {
		t.Error("expected an error for an unknown name")
	}
}

// TestRunInstallMultiArgFailsFastOnUnknownName verifies that when multiple
// app names are given, an invalid one at the end of the list prevents the
// manifest from being touched at all — including for the earlier, valid
// name that would otherwise have been auto-imported successfully.
func TestRunInstallMultiArgFailsFastOnUnknownName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	flagInstallForce = false
	flagInstallNoSave = false
	t.Cleanup(func() {
		flagInstallForce = false
		flagInstallNoSave = false
	})

	err := runInstall(installCmd, []string{"ripgrep", "typo-xyz-does-not-exist"})
	if err == nil {
		t.Fatal("expected an error for the unknown second argument")
	}

	if _, statErr := os.Stat(filepath.Join(dir, "paq", "config.toml")); !os.IsNotExist(statErr) {
		t.Fatal("manifest should not have been written when a later argument is invalid")
	}
}
