package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/ghtoken"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
)

// withJSONFlag sets flagJSON for the duration of the test and restores both it
// and ui.Global afterwards. PersistentPreRunE writes ui.Global as a side
// effect, so restoring flagJSON alone leaks JSON mode into every later test of
// this package.
func withJSONFlag(t *testing.T, v bool) {
	t.Helper()
	savedFlag, savedUI := flagJSON, ui.Global
	t.Cleanup(func() { flagJSON, ui.Global = savedFlag, savedUI })
	flagJSON = v
}

func TestPersistentPreRunERejectsJSONOnUnsupportedCommands(t *testing.T) {
	withJSONFlag(t, true)

	if err := rootCmd.PersistentPreRunE(installCmd, nil); err == nil {
		t.Error("expected --json to be rejected on `paq install`, got nil error")
	}
	if err := rootCmd.PersistentPreRunE(upgradeCmd, nil); err == nil {
		t.Error("expected --json to be rejected on `paq upgrade`, got nil error")
	}
}

func TestPersistentPreRunEAllowsJSONOnSupportedCommands(t *testing.T) {
	withJSONFlag(t, true)

	if err := rootCmd.PersistentPreRunE(lsCmd, nil); err != nil {
		t.Errorf("expected --json to be allowed on `paq ls`, got: %v", err)
	}
	if err := rootCmd.PersistentPreRunE(registryShowCmd, nil); err != nil {
		t.Errorf("expected --json to be allowed on `paq registry show`, got: %v", err)
	}
	// doctor is the diagnostic surface: it must be scriptable/CI-gateable like
	// the other read-only commands, not excluded from --json.
	if err := rootCmd.PersistentPreRunE(doctorCmd, nil); err != nil {
		t.Errorf("expected --json to be allowed on `paq doctor`, got: %v", err)
	}
}

func TestPersistentPreRunEAllowsNonJSONInvocations(t *testing.T) {
	withJSONFlag(t, false)

	if err := rootCmd.PersistentPreRunE(installCmd, nil); err != nil {
		t.Errorf("expected no error without --json, got: %v", err)
	}
}

// TestPersistentPreRunEAppliesUIConfig verifies that the flags are actually
// transferred to the global UI config: it is the only effect the hook has
// besides the --json rejection, and nothing else covered it.
func TestPersistentPreRunEAppliesUIConfig(t *testing.T) {
	withJSONFlag(t, true)
	savedQuiet, savedDebug := flagQuiet, flagDebug
	t.Cleanup(func() { flagQuiet, flagDebug = savedQuiet, savedDebug })
	flagQuiet, flagDebug = true, true

	if err := rootCmd.PersistentPreRunE(lsCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	if !ui.Global.JSON || !ui.Global.Quiet || !ui.Global.Debug {
		t.Errorf("ui.Global = %+v, want JSON, Quiet and Debug set", ui.Global)
	}
	if !ui.Global.Verbose {
		t.Error("--debug must imply verbose output")
	}
}

// TestApplyConfigPathOverride verifies --config and PAQ_CONFIG both set
// config.PathOverride (flag wins over env), and that neither being set clears
// a value left over from an earlier command in the same process.
func TestApplyConfigPathOverride(t *testing.T) {
	savedFlag, savedOverride := flagConfig, config.PathOverride
	t.Cleanup(func() { flagConfig, config.PathOverride = savedFlag, savedOverride })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this one on Windows

	flagConfig = filepath.Join(home, "from-flag.toml")
	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	if config.PathOverride != flagConfig {
		t.Errorf("PathOverride = %q, want %q (from --config)", config.PathOverride, flagConfig)
	}

	flagConfig = ""
	t.Setenv("PAQ_CONFIG", "~/from-env.toml")
	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	want := filepath.Join(home, "from-env.toml")
	if config.PathOverride != want {
		t.Errorf("PathOverride = %q, want %q (from PAQ_CONFIG, ~ expanded)", config.PathOverride, want)
	}

	t.Setenv("PAQ_CONFIG", "")
	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	if config.PathOverride != "" {
		t.Errorf("PathOverride = %q, want empty once neither --config nor PAQ_CONFIG is set", config.PathOverride)
	}
}

// TestApplyGitHubTokenEnv verifies `[defaults] github_token_env` overrides
// GITHUB_TOKEN when the named variable is set, and leaves it alone otherwise.
func TestApplyGitHubTokenEnv(t *testing.T) {
	savedOverride := config.PathOverride
	t.Cleanup(func() { config.PathOverride = savedOverride })

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[defaults]\ngithub_token_env = \"GH_MY_TOKEN\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config.PathOverride = path

	t.Setenv("GITHUB_TOKEN", "default")
	t.Setenv("GH_MY_TOKEN", "")
	applyGitHubTokenEnv()
	if got := os.Getenv("GITHUB_TOKEN"); got != "default" {
		t.Errorf("GITHUB_TOKEN = %q, want %q (GH_MY_TOKEN unset)", got, "default")
	}

	t.Setenv("GH_MY_TOKEN", "custom")
	applyGitHubTokenEnv()
	if got := os.Getenv("GITHUB_TOKEN"); got != "custom" {
		t.Errorf("GITHUB_TOKEN = %q, want %q (from GH_MY_TOKEN)", got, "custom")
	}
}

// withLocalModeReset saves and restores every global local mode touches.
func withLocalModeReset(t *testing.T) {
	t.Helper()
	savedFlag, savedConfig := flagLocal, flagConfig
	savedOverride, savedRoot, savedState := config.PathOverride, config.ProjectRoot, state.PathOverride
	t.Cleanup(func() {
		flagLocal, flagConfig = savedFlag, savedConfig
		config.PathOverride, config.ProjectRoot, state.PathOverride = savedOverride, savedRoot, savedState
	})
	flagLocal, flagConfig = false, ""
	t.Setenv("PAQ_LOCAL", "")
	t.Setenv("PAQ_CONFIG", "")
}

// TestApplyConfigPathOverrideLocal verifies --local finds the nearest
// paq.toml upwards and moves manifest and state next to it, ignoring
// PAQ_CONFIG.
func TestApplyConfigPathOverrideLocal(t *testing.T) {
	withLocalModeReset(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "paq.toml"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	t.Setenv("PAQ_CONFIG", filepath.Join(root, "ignored.toml"))

	flagLocal = true
	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	if config.ProjectRoot != root {
		t.Errorf("ProjectRoot = %q, want %q", config.ProjectRoot, root)
	}
	if want := filepath.Join(root, "paq.toml"); config.PathOverride != want {
		t.Errorf("PathOverride = %q, want %q", config.PathOverride, want)
	}
	if want := filepath.Join(root, ".paq", "state.json"); state.PathOverride != want {
		t.Errorf("state.PathOverride = %q, want %q", state.PathOverride, want)
	}

	// Leaving local mode clears the overrides again.
	flagLocal = false
	t.Setenv("PAQ_CONFIG", "")
	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	if config.ProjectRoot != "" || state.PathOverride != "" || config.PathOverride != "" {
		t.Errorf("overrides not cleared: root %q, state %q, config %q", config.ProjectRoot, state.PathOverride, config.PathOverride)
	}
}

// TestApplyConfigPathOverrideLocalWithoutManifest verifies PAQ_LOCAL enables
// local mode and, with no paq.toml anywhere upwards, roots the project at the
// working directory.
func TestApplyConfigPathOverrideLocalWithoutManifest(t *testing.T) {
	withLocalModeReset(t)
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	t.Setenv("PAQ_LOCAL", "1")

	if err := applyConfigPathOverride(); err != nil {
		t.Fatalf("applyConfigPathOverride: %v", err)
	}
	if config.ProjectRoot != cwd {
		t.Errorf("ProjectRoot = %q, want the working directory %q", config.ProjectRoot, cwd)
	}
}

func TestApplyConfigPathOverrideLocalRejectsConfig(t *testing.T) {
	withLocalModeReset(t)
	flagLocal, flagConfig = true, "other.toml"

	if err := applyConfigPathOverride(); err == nil {
		t.Error("expected --config to be rejected together with --local")
	}
}

// TestApplyGitHubTokenEnvPerSpec verifies a spec's github_token_env is sent
// for that spec's repo only, and is skipped while its variable is unset.
func TestApplyGitHubTokenEnvPerSpec(t *testing.T) {
	savedOverride := config.PathOverride
	t.Cleanup(func() { config.PathOverride = savedOverride })

	path := filepath.Join(t.TempDir(), "config.toml")
	manifest := "[specs.own]\nbackend = \"github\"\nrepo = \"Org/OwnToken\"\ngithub_token_env = \"GH_ORG_TOKEN\"\n" +
		"[specs.unset]\nbackend = \"github\"\nrepo = \"org/unset-token\"\ngithub_token_env = \"GH_UNSET_TOKEN\"\n"
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	config.PathOverride = path

	t.Setenv("GITHUB_TOKEN", "default")
	t.Setenv("GH_ORG_TOKEN", "org")
	t.Setenv("GH_UNSET_TOKEN", "")
	applyGitHubTokenEnv()

	for repo, want := range map[string]string{"org/owntoken": "org", "org/unset-token": "default", "other/repo": "default"} {
		if got := ghtoken.For(repo); got != want {
			t.Errorf("ghtoken.For(%q) = %q, want %q", repo, got, want)
		}
	}
}
