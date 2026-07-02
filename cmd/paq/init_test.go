package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func resetInitFlag(t *testing.T) {
	t.Helper()
	flagInitForce = false
	t.Cleanup(func() { flagInitForce = false })
}

func TestRunInitCreatesManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	resetInitFlag(t)

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	path := filepath.Join(dir, "paq", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	// The skeleton is fully commented out: it must parse as valid, empty TOML.
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("generated skeleton is not valid TOML: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("expected an empty parsed manifest (all-comment skeleton), got %v", raw)
	}
}

func TestRunInitRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	resetInitFlag(t)

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("first runInit: %v", err)
	}

	path := filepath.Join(dir, "paq", "config.toml")
	if err := os.WriteFile(path, []byte("# user was here\n"), 0644); err != nil {
		t.Fatalf("simulate user edit: %v", err)
	}

	if err := runInit(initCmd, nil); err == nil {
		t.Fatal("expected an error when the manifest already exists")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if string(data) != "# user was here\n" {
		t.Errorf("existing manifest was overwritten without --force, got:\n%s", data)
	}
}

func TestRunInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	resetInitFlag(t)

	path := filepath.Join(dir, "paq", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# old content\n"), 0644); err != nil {
		t.Fatalf("seed existing manifest: %v", err)
	}

	flagInitForce = true
	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit --force: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if string(data) == "# old content\n" {
		t.Error("expected --force to overwrite the existing manifest")
	}
}
