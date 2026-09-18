package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const registryShowTestManifest = `[specs.mytool]
backend = "github"
repo = "owner/mytool"
asset = "mytool-{{version}}-{{os}}-{{arch}}.tar.gz"
archive = "tar.gz"
`

func TestRunRegistryShowPrintsSpecDetails(t *testing.T) {
	doctorEnv(t, registryShowTestManifest)

	out := captureStdout(t, func() {
		if err := runRegistryShow(registryShowCmd, []string{"mytool"}); err != nil {
			t.Fatalf("runRegistryShow: %v", err)
		}
	})
	for _, want := range []string{"mytool", "github", "owner/mytool", "tar.gz"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunRegistryShowUnknownSpecSuggestsSimilar(t *testing.T) {
	doctorEnv(t, registryShowTestManifest)

	err := runRegistryShow(registryShowCmd, []string{"mytoo"})
	if err == nil {
		t.Fatal("runRegistryShow: nil error, want one for an unknown spec")
	}
	he, ok := err.(hintError)
	if !ok {
		t.Fatalf("error = %v (%T), want a hintError", err, err)
	}
	if !strings.Contains(he.hint, "mytool") {
		t.Errorf("hint = %q, want it to suggest the similarly-named %q", he.hint, "mytool")
	}
}

func TestRunRegistryShowJSON(t *testing.T) {
	doctorEnv(t, registryShowTestManifest)

	out := withJSON(t, func() {
		if err := runRegistryShow(registryShowCmd, []string{"mytool"}); err != nil {
			t.Fatalf("runRegistryShow: %v", err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput:\n%s", err, out)
	}
	if got["name"] != "mytool" {
		t.Errorf(`json["name"] = %v, want "mytool"`, got["name"])
	}
	spec, ok := got["spec"].(map[string]any)
	if !ok {
		t.Fatalf(`json["spec"] is not an object: %v`, got["spec"])
	}
	// config.Spec has no json tags: JSON output uses the Go field names.
	if spec["Repo"] != "owner/mytool" {
		t.Errorf(`json["spec"]["Repo"] = %v, want "owner/mytool"`, spec["Repo"])
	}
}
