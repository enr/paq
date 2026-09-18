package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const configShowTestManifest = `[defaults]
bin = "~/.custom/bin"
opt = "~/.custom/opt"

[apps.rg]
use = "ripgrep"
version = "14.1.1"
dest = "~/.custom/bin/rg"
`

func TestRunConfigShowPrintsPathAndApps(t *testing.T) {
	doctorEnv(t, configShowTestManifest)

	out := captureStdout(t, func() {
		if err := runConfigShow(configShowCmd, nil); err != nil {
			t.Fatalf("runConfigShow: %v", err)
		}
	})

	for _, want := range []string{"Config", "Defaults", "~/.custom/bin", "~/.custom/opt", "rg", "14.1.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunConfigShowNoManifestUsesBuiltinDefaults(t *testing.T) {
	doctorEnv(t, "")

	out := captureStdout(t, func() {
		if err := runConfigShow(configShowCmd, nil); err != nil {
			t.Fatalf("runConfigShow: %v", err)
		}
	})
	if !strings.Contains(out, "not found") {
		t.Errorf("output = %q, want it to note the manifest is missing", out)
	}
	if !strings.Contains(out, "none configured") {
		t.Errorf("output = %q, want it to note no apps are configured", out)
	}
}

func TestRunConfigShowJSON(t *testing.T) {
	doctorEnv(t, configShowTestManifest)

	out := withJSON(t, func() {
		if err := runConfigShow(configShowCmd, nil); err != nil {
			t.Fatalf("runConfigShow: %v", err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput:\n%s", err, out)
	}
	for _, key := range []string{"path", "exists", "defaults", "effective_defaults", "registry", "registry_cache", "apps"} {
		if _, ok := got[key]; !ok {
			t.Errorf("json missing key %q: %v", key, got)
		}
	}
	if got["exists"] != true {
		t.Errorf(`json["exists"] = %v, want true`, got["exists"])
	}
}
