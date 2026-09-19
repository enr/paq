//go:build linux

// Env mapping is keyed by the canonical {{env}} value, which platform.Detect
// only ever sets on Linux ("gnu"). These tests therefore describe Linux
// behaviour and are built only there; darwin and windows need their own
// equivalents (see AUDIT-TESTS.md §8).
package install

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
)

// TestPipelineAppliesEnvMapping verifies that [x.env] (and the app-level
// override) affects {{env}} in the source URL template, mirroring how
// spec.OS/spec.Arch are applied.
func TestPipelineAppliesEnvMapping(t *testing.T) {
	isolateState(t)
	fileContent := []byte("payload")
	zipData := makeFakeZip("tool-1.0.0", "bin/tool", fileContent)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write(zipData)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend:         "url",
				Source:          srv.URL + "/tool-{{version}}-{{env}}.zip",
				Archive:         "zip",
				StripComponents: 1,
				Env:             map[string]string{"gnu": "musl"},
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: t.TempDir()},
		},
	}

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if !strings.Contains(gotPath, "musl") {
		t.Errorf("request path = %q, want it to contain \"musl\"", gotPath)
	}

	// App-level env override takes precedence over the spec's.
	gotPath = ""
	cfg.Apps["tool"] = config.AppEntry{
		Use: "tool", Version: "1.0.0", Dest: t.TempDir(),
		Env: map[string]string{"gnu": "override"},
	}
	if err := Run(context.Background(), cfg, "tool", nil, &Hooks{Force: true}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if !strings.Contains(gotPath, "override") {
		t.Errorf("request path = %q, want it to contain \"override\"", gotPath)
	}
}

// TestPipelineAppliesEnvArchMapping verifies that [x.env_arch] overrides
// {{env}} for the matching arch only, so a tool can ship different C
// environments per arch (e.g. musl on x86_64, gnu on aarch64).
func TestPipelineAppliesEnvArchMapping(t *testing.T) {
	isolateState(t)
	fileContent := []byte("payload")
	zipData := makeFakeZip("tool-1.0.0", "bin/tool", fileContent)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write(zipData)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend:         "url",
				Source:          srv.URL + "/tool-{{version}}-{{env}}.zip",
				Archive:         "zip",
				StripComponents: 1,
				EnvArch:         map[string]string{runtime.GOARCH: "musl", "other": "wrong"},
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: t.TempDir()},
		},
	}

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if !strings.Contains(gotPath, "musl") {
		t.Errorf("request path = %q, want it to contain \"musl\"", gotPath)
	}
}
