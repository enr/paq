package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
)

const infoTestManifest = `[apps.tool]
use = "tool"
version = "1.0.0"
dest = "/opt/tool"

[specs.tool]
backend = "url"
source = "https://example.com/tool-{{version}}.tar.gz"
archive = "tar.gz"
`

func TestRunInfoPrintsAppAndSpecDetails(t *testing.T) {
	doctorEnv(t, infoTestManifest)

	out := captureStdout(t, func() {
		if err := runInfo(infoCmd, []string{"tool"}); err != nil {
			t.Fatalf("runInfo: %v", err)
		}
	})

	for _, want := range []string{"tool", "1.0.0", "/opt/tool", "url", "example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunInfoUnknownAppReturnsHintError(t *testing.T) {
	doctorEnv(t, infoTestManifest)

	err := runInfo(infoCmd, []string{"nope"})
	if err == nil {
		t.Fatal("runInfo: nil error, want one for an app not in the manifest")
	}
	if _, ok := err.(hintError); !ok {
		t.Errorf("error = %v (%T), want a hintError with a `paq ls` hint", err, err)
	}
}

func TestRunInfoJSONIncludesInstalledAndSpec(t *testing.T) {
	doctorEnv(t, infoTestManifest)

	out := withJSON(t, func() {
		if err := runInfo(infoCmd, []string{"tool"}); err != nil {
			t.Fatalf("runInfo: %v", err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput:\n%s", err, out)
	}
	if got["name"] != "tool" {
		t.Errorf(`json["name"] = %v, want "tool"`, got["name"])
	}
	if _, ok := got["spec"]; !ok {
		t.Error(`json["spec"] missing`)
	}
	if _, ok := got["installed"]; !ok {
		t.Error(`json["installed"] missing`)
	}
}

// TestRunInfoShowsLockedVersion ties `info` to paq.lock.toml: an app that
// tracks "latest" and has a lock entry must surface it, both for a human and
// a scripted reader.
func TestRunInfoShowsLockedVersion(t *testing.T) {
	doctorEnv(t, `[apps.tool]
use = "tool"
version = "latest"

[specs.tool]
backend = "url"
source = "https://example.com/tool-{{version}}.tar.gz"
archive = "tar.gz"
`)
	if err := config.WriteLockEntry("tool", config.LockEntry{Version: "2.5.0", SHA256: "deadbeef"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runInfo(infoCmd, []string{"tool"}); err != nil {
			t.Fatalf("runInfo: %v", err)
		}
	})
	if !strings.Contains(out, "2.5.0") {
		t.Errorf("output missing the locked version 2.5.0:\n%s", out)
	}

	jsonOut := withJSON(t, func() {
		if err := runInfo(infoCmd, []string{"tool"}); err != nil {
			t.Fatalf("runInfo: %v", err)
		}
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput:\n%s", err, jsonOut)
	}
	if got["locked_version"] != "2.5.0" {
		t.Errorf(`json["locked_version"] = %v, want "2.5.0"`, got["locked_version"])
	}
}
