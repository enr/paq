package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/template"
)

func TestPrintLsTableJSON(t *testing.T) {
	entries := []LsEntry{
		{InstalledApp: state.InstalledApp{Name: "rg", Version: "14.0.0", Kind: "file", Dest: "/opt/bin"}},
	}
	out, _ := withGlobal(t, Config{JSON: true}, func() { PrintLsTable(entries) })

	if !strings.Contains(out, `"name": "rg"`) || !strings.Contains(out, `"missing": false`) {
		t.Errorf("PrintLsTable JSON = %q, want it to contain the entry fields", out)
	}
}

func TestPrintLsTablePlain(t *testing.T) {
	entries := []LsEntry{
		{InstalledApp: state.InstalledApp{Name: "rg", Version: "14.0.0", Kind: "file", Dest: "/opt/bin"}},
	}
	out, _ := withGlobal(t, Config{}, func() { PrintLsTable(entries) })

	for _, want := range []string{"NAME", "VERSION", "KIND", "DEST", "rg", "14.0.0", "file", "/opt/bin"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintLsTable plain output = %q, want it to contain %q", out, want)
		}
	}
}

func TestPrintAvailableTableJSON(t *testing.T) {
	entries := []RegistryEntry{{Name: "rg", Backend: "github", Repo: "BurntSushi/ripgrep", Source: "embedded"}}
	out, _ := withGlobal(t, Config{JSON: true}, func() { PrintAvailableTable(entries) })

	if !strings.Contains(out, `"name": "rg"`) {
		t.Errorf("PrintAvailableTable JSON = %q, want it to contain the entry", out)
	}
}

func TestPrintAvailableTablePlain(t *testing.T) {
	entries := []RegistryEntry{
		{Name: "rg", Backend: "github", Repo: "BurntSushi/ripgrep", Source: "embedded"},
		{Name: "custom", Backend: "", Repo: "", Source: ""},
	}
	out, _ := withGlobal(t, Config{}, func() { PrintAvailableTable(entries) })

	for _, want := range []string{"NAME", "BACKEND", "SOURCE", "REPO", "rg", "github", "embedded", "BurntSushi/ripgrep"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintAvailableTable plain output = %q, want it to contain %q", out, want)
		}
	}
	// Empty fields render as "-" rather than blank.
	if !strings.Contains(out, "custom") || !strings.Contains(out, "-") {
		t.Errorf("PrintAvailableTable plain output = %q, want empty cells rendered as \"-\"", out)
	}
}

func TestPrintOutdatedTableJSON(t *testing.T) {
	entries := []OutdatedEntry{{Name: "rg", Installed: "13.0.0", Latest: "14.0.0"}}
	out, _ := withGlobal(t, Config{JSON: true}, func() { PrintOutdatedTable(entries) })

	if !strings.Contains(out, `"name": "rg"`) || !strings.Contains(out, `"latest": "14.0.0"`) {
		t.Errorf("PrintOutdatedTable JSON = %q, want it to contain the entry", out)
	}
}

func TestPrintOutdatedTableEmpty(t *testing.T) {
	out, _ := withGlobal(t, Config{}, func() { PrintOutdatedTable(nil) })

	want := "All tools are up to date.\n"
	if out != want {
		t.Errorf("PrintOutdatedTable(nil) = %q, want %q", out, want)
	}
}

func TestPrintOutdatedTablePlain(t *testing.T) {
	entries := []OutdatedEntry{{Name: "rg", Installed: "13.0.0", Latest: "14.0.0"}}
	out, _ := withGlobal(t, Config{}, func() { PrintOutdatedTable(entries) })

	for _, want := range []string{"APP", "INSTALLED", "LATEST", "rg", "13.0.0", "14.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintOutdatedTable plain output = %q, want it to contain %q", out, want)
		}
	}
}

func TestPrintConfigShowJSON(t *testing.T) {
	apps := map[string]config.AppEntry{"rg": {Use: "rg", Version: "latest"}}
	out, _ := withGlobal(t, Config{JSON: true}, func() {
		PrintConfigShow("/home/u/.config/paq.toml", true, config.Defaults{Bin: "/opt/bin"}, "/opt/bin", "/opt/opt",
			apps, config.RegistrySettings{}, "/home/u/.cache/paq/registry", nil, nil)
	})

	for _, want := range []string{`"path": "/home/u/.config/paq.toml"`, `"exists": true`, `"rg"`} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintConfigShow JSON = %q, want it to contain %q", out, want)
		}
	}
}

func TestPrintConfigShowPlainNoApps(t *testing.T) {
	out, _ := withGlobal(t, Config{}, func() {
		PrintConfigShow("/home/u/.config/paq.toml", false, config.Defaults{}, "/opt/bin", "/opt/opt",
			nil, config.RegistrySettings{}, "/home/u/.cache/paq/registry", nil, nil)
	})

	for _, want := range []string{"Config:", "not found", "Defaults", "Registry", "not installed", "Apps", "none configured"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintConfigShow plain output = %q, want it to contain %q", out, want)
		}
	}
}

func TestPrintConfigShowPlainWithAppsAndRegistryMeta(t *testing.T) {
	apps := map[string]config.AppEntry{
		"rg": {Use: "rg", Version: "14.0.0", Dest: "/opt/bin"},
		"jq": {}, // exercises the "use defaults to key" / "(default)" fallbacks
	}
	meta := &registry.Meta{Version: "2024-01-01", FetchedAt: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}
	out, _ := withGlobal(t, Config{}, func() {
		PrintConfigShow("/home/u/.config/paq.toml", true, config.Defaults{Bin: "/opt/bin"}, "/opt/bin", "/opt/opt",
			apps, config.RegistrySettings{URL: "https://example.com", PublicKey: "abc"}, "/home/u/.cache/paq/registry", meta, nil)
	})

	for _, want := range []string{"rg", "14.0.0", "jq", "(default)", "https://example.com", "2024-01-01"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintConfigShow plain output = %q, want it to contain %q", out, want)
		}
	}
}

func TestPrintConfigShowPlainRegistryOpenErr(t *testing.T) {
	out, _ := withGlobal(t, Config{}, func() {
		PrintConfigShow("/home/u/.config/paq.toml", true, config.Defaults{}, "/opt/bin", "/opt/opt",
			nil, config.RegistrySettings{}, "/home/u/.cache/paq/registry", nil, errBadRegistry)
	})

	if !strings.Contains(out, "unusable") {
		t.Errorf("PrintConfigShow plain output = %q, want it to mention the registry open error", out)
	}
}

var errBadRegistry = &stringError{"corrupt cache"}

type stringError struct{ msg string }

func (e *stringError) Error() string { return e.msg }

func TestPrintInfoDetailJSON(t *testing.T) {
	spec := config.Spec{Backend: "github", Repo: "BurntSushi/ripgrep"}
	app := config.AppEntry{Version: "14.0.0"}
	out, _ := withGlobal(t, Config{JSON: true}, func() {
		PrintInfoDetail("rg", spec, app, nil, "", template.Vars{})
	})

	if !strings.Contains(out, `"name": "rg"`) || !strings.Contains(out, `"Backend": "github"`) {
		t.Errorf("PrintInfoDetail JSON = %q, want it to contain name and spec", out)
	}
}

func TestPrintInfoDetailPlainNotInstalled(t *testing.T) {
	spec := config.Spec{Backend: "github", Repo: "BurntSushi/ripgrep", Asset: "rg-{{version}}.tar.gz"}
	app := config.AppEntry{Use: "rg", Version: "latest"}
	out, _ := withGlobal(t, Config{}, func() {
		PrintInfoDetail("rg", spec, app, nil, "", template.Vars{})
	})

	for _, want := range []string{"App:", "rg", "Backend:", "github", "(not installed)"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintInfoDetail plain output = %q, want it to contain %q", out, want)
		}
	}
}

func TestPrintInfoDetailPlainInstalledAndLocked(t *testing.T) {
	spec := config.Spec{
		Backend: "github", Repo: "BurntSushi/ripgrep",
		Asset:    "rg-{{version}}-{{arch}}.tar.gz",
		Binaries: []config.Binary{{From: "rg", To: "rg"}},
	}
	app := config.AppEntry{Use: "rg", Version: "latest"}
	installed := []state.InstalledApp{
		{Name: "rg", Version: "14.0.0", Kind: "binaries", Dest: "/opt/bin", InstalledAt: time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)},
	}
	vars := template.Vars{Version: "14.0.0", Arch: "amd64"}
	out, _ := withGlobal(t, Config{}, func() {
		PrintInfoDetail("rg", spec, app, installed, "14.0.0", vars)
	})

	for _, want := range []string{"Locked ver:", "14.0.0 (paq.lock.toml)", "rg-14.0.0-amd64.tar.gz", "rg → rg", "Installed ver:", "binaries"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintInfoDetail plain output = %q, want it to contain %q", out, want)
		}
	}
}

func TestPrintSpecDetailJSON(t *testing.T) {
	spec := config.Spec{Backend: "github", Repo: "BurntSushi/ripgrep"}
	out, _ := withGlobal(t, Config{JSON: true}, func() { PrintSpecDetail("rg", spec) })

	if !strings.Contains(out, `"name": "rg"`) || !strings.Contains(out, `"Repo": "BurntSushi/ripgrep"`) {
		t.Errorf("PrintSpecDetail JSON = %q, want it to contain name and spec", out)
	}
}

func TestPrintSpecDetailPlain(t *testing.T) {
	spec := config.Spec{
		Backend: "github", Repo: "BurntSushi/ripgrep", DefaultVersion: "14.0.0",
		Binaries:        []config.Binary{{From: "rg"}, {To: "rg2"}},
		OS:              map[string]string{"linux": "unknown-linux-gnu"},
		Arch:            map[string]string{"amd64": "x86_64"},
		Env:             map[string]string{"gnu": "gnu"},
		Platforms:       []string{"linux/amd64", "darwin"},
		StripComponents: 1,
	}
	out, _ := withGlobal(t, Config{}, func() { PrintSpecDetail("rg", spec) })

	for _, want := range []string{
		"Spec:", "rg", "Default version:", "14.0.0",
		"Binaries:", "rg", "rg2", "Strip:", "1",
		"OS:", "linux = unknown-linux-gnu",
		"Arch:", "amd64 = x86_64",
		"Env:", "gnu = gnu",
		"Platforms:", "linux/amd64, darwin",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintSpecDetail plain output = %q, want it to contain %q", out, want)
		}
	}
}

func TestFormatBinaries(t *testing.T) {
	cases := []struct {
		name string
		bins []config.Binary
		want []string
	}{
		{"empty", nil, nil},
		{"from and to", []config.Binary{{From: "rg", To: "bin/rg"}}, []string{"rg → bin/rg"}},
		{"from only", []config.Binary{{From: "rg"}}, []string{"rg"}},
		{"to only", []config.Binary{{To: "rg"}}, []string{"rg"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatBinaries(tc.bins)
			if len(got) != len(tc.want) {
				t.Fatalf("formatBinaries(%v) = %v, want %v", tc.bins, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("formatBinaries(%v)[%d] = %q, want %q", tc.bins, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestResolveOrRaw(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		vars template.Vars
		want string
	}{
		{"empty", "", template.Vars{}, ""},
		{"unresolved version placeholder, no pinned version", "rg-{{version}}.tar.gz", template.Vars{}, "rg-{{version}}.tar.gz"},
		{"resolved", "rg-{{version}}.tar.gz", template.Vars{Version: "14.0.0"}, "rg-14.0.0.tar.gz"},
		{"no placeholder", "rg.tar.gz", template.Vars{}, "rg.tar.gz"},
		{"unknown placeholder falls back to raw", "rg-{{nope}}.tar.gz", template.Vars{}, "rg-{{nope}}.tar.gz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveOrRaw(tc.raw, tc.vars)
			if got != tc.want {
				t.Errorf("resolveOrRaw(%q, %+v) = %q, want %q", tc.raw, tc.vars, got, tc.want)
			}
		})
	}
}

func TestSortedPackages(t *testing.T) {
	in := []state.InstalledApp{
		{Name: "rg", Version: "14.0.0"},
		{Name: "jq", Version: "1.7"},
		{Name: "rg", Version: "13.0.0"},
	}
	got := sortedPackages(in)

	want := []string{"jq@1.7", "rg@13.0.0", "rg@14.0.0"}
	if len(got) != len(want) {
		t.Fatalf("sortedPackages(%v) has %d entries, want %d", in, len(got), len(want))
	}
	for i, w := range want {
		if got[i].Name+"@"+got[i].Version != w {
			t.Errorf("sortedPackages(%v)[%d] = %s@%s, want %s", in, i, got[i].Name, got[i].Version, w)
		}
	}
	// Input slice must not be mutated.
	if in[0].Name != "rg" || in[0].Version != "14.0.0" {
		t.Errorf("sortedPackages mutated its input: %v", in)
	}
}
