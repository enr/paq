//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/enr/paq/embedded"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/install"
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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

	out, err := exec.Command(dest, "--version").Output()
	if err != nil {
		t.Fatalf("rg --version failed: %v", err)
	}
	t.Logf("rg --version: %s", out)
}
