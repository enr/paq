package main

import (
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func TestRenderAppEntryTOML(t *testing.T) {
	entry := config.AppEntry{Use: "ripgrep", Version: "latest", Dest: "~/.local/bin/rg{{ext}}"}
	block := renderAppEntryTOML("rg", entry)

	for _, want := range []string{"[apps.rg]", `use = "ripgrep"`, `version = "latest"`, `dest = "~/.local/bin/rg{{ext}}"`} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q; got:\n%s", want, block)
		}
	}

	// Il blocco generato deve essere TOML valido e ri-parsabile.
	var raw struct {
		Apps map[string]config.AppEntry `toml:"apps"`
	}
	if err := toml.Unmarshal([]byte(block), &raw); err != nil {
		t.Fatalf("generated block is not valid TOML: %v", err)
	}
	got := raw.Apps["rg"]
	if got.Use != "ripgrep" || got.Version != "latest" || got.Dest != "~/.local/bin/rg{{ext}}" {
		t.Errorf("parsed entry = %+v", got)
	}
}

func TestRenderAppEntryTOMLOmitsEmpty(t *testing.T) {
	block := renderAppEntryTOML("x", config.AppEntry{Use: "x", Version: "latest"})
	if strings.Contains(block, "dest") {
		t.Errorf("empty dest should be omitted; got:\n%s", block)
	}
}

func TestValidAppKey(t *testing.T) {
	valid := []string{"rg", "ripgrep", "go-task", "tool_1", "JDK21"}
	invalid := []string{"", "with space", "dot.ted", "a/b", "quote\"d"}
	for _, k := range valid {
		if !validAppKey(k) {
			t.Errorf("validAppKey(%q) = false, want true", k)
		}
	}
	for _, k := range invalid {
		if validAppKey(k) {
			t.Errorf("validAppKey(%q) = true, want false", k)
		}
	}
}
