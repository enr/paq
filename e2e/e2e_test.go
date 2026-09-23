//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/enr/paq/embedded"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/state"
)

func loadE2ECfg(t *testing.T, apps map[string]config.AppEntry) *config.Config {
	t.Helper()
	registry, err := config.LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		t.Fatal(err)
	}
	globalTmpl, globalTmplOS, err := config.LoadGlobalTemplates(embedded.RegistryFS)
	if err != nil {
		t.Fatal(err)
	}
	userCfg := &config.Config{
		Apps:              apps,
		GlobalTemplates:   globalTmpl,
		GlobalTemplatesOS: globalTmplOS,
	}
	cfg, err := config.Merge(registry, userCfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestInstallRipgrep(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", stateHome)
	}
	dest := filepath.Join(t.TempDir(), "rg")
	cfg := loadE2ECfg(t, map[string]config.AppEntry{
		"rg": {
			Use:     "ripgrep",
			Version: "latest",
			Dest:    dest,
		},
	})

	if err := install.Run(context.Background(), cfg, "rg", nil, nil); err != nil {
		t.Fatalf("install rg failed: %v", err)
	}

	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("rg not found at %s: %v", dest, err)
	}

	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	recs := st.ByName("rg")
	if len(recs) != 1 {
		t.Fatalf("state has %d records for rg, want 1", len(recs))
	}

	out, err := exec.Command(dest, "--version").Output()
	if err != nil {
		t.Fatalf("rg --version failed: %v", err)
	}
	if !strings.Contains(string(out), recs[0].Version) {
		t.Errorf("rg --version = %q, want it to report the installed version %s", out, recs[0].Version)
	}
	if !strings.HasPrefix(string(out), "ripgrep ") {
		t.Errorf("rg --version = %q, want a ripgrep banner", out)
	}
}
