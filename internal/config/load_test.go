package config

import (
	"strings"
	"testing"

	"github.com/enr/paq/embedded"
	"github.com/pelletier/go-toml/v2"
)

func TestLoadEmbeddedRegistry(t *testing.T) {
	specs, err := LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		t.Fatal(err)
	}

	// ripgrep deve essere presente
	rg, ok := specs["ripgrep"]
	if !ok {
		t.Fatal("ripgrep spec not found")
	}
	if rg.Backend != "github" {
		t.Errorf("ripgrep.Backend = %q, want github", rg.Backend)
	}
	if rg.Repo != "BurntSushi/ripgrep" {
		t.Errorf("ripgrep.Repo = %q, want BurntSushi/ripgrep", rg.Repo)
	}
	if rg.Archive != "tar.gz" {
		t.Errorf("ripgrep.Archive = %q, want tar.gz", rg.Archive)
	}
	if rg.Arch["amd64"] != "x86_64" {
		t.Errorf("ripgrep.Arch[amd64] = %q, want x86_64", rg.Arch["amd64"])
	}

	// jdk deve essere presente
	jdk, ok := specs["jdk"]
	if !ok {
		t.Fatal("jdk spec not found")
	}
	if jdk.Backend != "url" {
		t.Errorf("jdk.Backend = %q, want url", jdk.Backend)
	}
	if jdk.StripComponents != 1 {
		t.Errorf("jdk.StripComponents = %d, want 1", jdk.StripComponents)
	}
	if jdk.OS["darwin"] != "macos" {
		t.Errorf("jdk.OS[darwin] = %q, want macos", jdk.OS["darwin"])
	}

	// override per-OS darwin
	darwinOv, ok := jdk.OSOverrides["darwin"]
	if !ok {
		t.Fatal("jdk darwin override not found")
	}
	if darwinOv.StripComponents == nil || *darwinOv.StripComponents != 0 {
		t.Errorf("jdk darwin StripComponents override = %v, want 0", darwinOv.StripComponents)
	}
	if darwinOv.Subdir != "*/Contents/Home" {
		t.Errorf("jdk darwin Subdir = %q, want */Contents/Home", darwinOv.Subdir)
	}

	// zipp should use multi-binary mode.
	zipp, ok := specs["zipp"]
	if !ok {
		t.Fatal("zipp spec not found")
	}
	if len(zipp.Binaries) != 3 {
		t.Fatalf("zipp.Binaries len = %d, want 3", len(zipp.Binaries))
	}
	if zipp.Binaries[0].From != "zipts{{ext}}" {
		t.Errorf("zipp.Binaries[0].From = %q, want zipts{{ext}}", zipp.Binaries[0].From)
	}
}

// TestRunpSpec verifies the runp spec (github backend, multi-platform zip asset).
func TestRunpSpec(t *testing.T) {
	specs, err := LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		t.Fatal(err)
	}

	runp, ok := specs["runp"]
	if !ok {
		t.Fatal("runp spec not found")
	}
	if runp.Backend != "github" {
		t.Errorf("runp.Backend = %q, want github", runp.Backend)
	}
	if runp.Repo != "enr/runp" {
		t.Errorf("runp.Repo = %q, want enr/runp", runp.Repo)
	}
	if runp.Asset != "runp-{{version}}_{{os}}_{{arch}}.zip" {
		t.Errorf("runp.Asset = %q, want runp-{{version}}_{{os}}_{{arch}}.zip", runp.Asset)
	}
	if runp.Archive != "zip" {
		t.Errorf("runp.Archive = %q, want zip", runp.Archive)
	}
	if runp.Extract != "runp{{ext}}" {
		t.Errorf("runp.Extract = %q, want runp{{ext}}", runp.Extract)
	}
	if runp.Verify.SHA256Asset != "runp-{{version}}_checksums.txt" {
		t.Errorf("runp.Verify.SHA256Asset = %q, want runp-{{version}}_checksums.txt", runp.Verify.SHA256Asset)
	}

	wantPlatforms := []string{"linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64"}
	for _, p := range wantPlatforms {
		os, arch, _ := strings.Cut(p, "/")
		if !runp.SupportsPlatform(os, arch) {
			t.Errorf("runp should support %s", p)
		}
	}
	// A platform not distributed must not be supported.
	if runp.SupportsPlatform("darwin", "amd64") {
		t.Error("runp should not support darwin/amd64")
	}
}

// TestWindowsArchiveOverride verifies that recipes distributing a zip on
// Windows (bat/ripgrep/delta/hugo) select it via the [<name>.windows] section.
func TestWindowsArchiveOverride(t *testing.T) {
	specs, err := LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"bat", "ripgrep", "delta", "hugo"} {
		r, ok := specs[name]
		if !ok {
			t.Fatalf("%s spec not found", name)
		}
		// Default (non-Windows): stays tar.gz.
		if r.Archive != "tar.gz" {
			t.Errorf("%s default Archive = %q, want tar.gz", name, r.Archive)
		}
		win := r.ApplyOSOverride("windows")
		if win.Archive != "zip" {
			t.Errorf("%s windows Archive = %q, want zip", name, win.Archive)
		}
		if got := win.Asset[len(win.Asset)-4:]; got != ".zip" {
			t.Errorf("%s windows Asset = %q, want .zip suffix", name, win.Asset)
		}
		// The override must not mutate the original spec.
		if r.Archive != "tar.gz" {
			t.Errorf("%s Archive mutated to %q after ApplyOSOverride", name, r.Archive)
		}
	}
}

// TestParseSpecBinariesRename verifies the parsing round-trip of a spec
// with binaries that uses the optional "to" field.
func TestParseSpecBinariesRename(t *testing.T) {
	data := []byte(`
[tool]
backend = "github"
repo = "owner/tool"
archive = "zip"
binaries = [
  { from = "a{{ext}}" },
  { from = "b{{ext}}", to = "bb{{ext}}" },
]
`)
	specs, err := parseSpecFile(data)
	if err != nil {
		t.Fatal(err)
	}
	r := specs["tool"]
	if len(r.Binaries) != 2 {
		t.Fatalf("Binaries len = %d, want 2", len(r.Binaries))
	}
	if r.Binaries[0].From != "a{{ext}}" || r.Binaries[0].To != "" {
		t.Errorf("Binaries[0] = %+v, want {From:a{{ext}} To:}", r.Binaries[0])
	}
	if r.Binaries[1].From != "b{{ext}}" || r.Binaries[1].To != "bb{{ext}}" {
		t.Errorf("Binaries[1] = %+v, want {From:b{{ext}} To:bb{{ext}}}", r.Binaries[1])
	}
}

// TestUserConfigSpecs verifies that a user-defined recipe in the manifest's
// [specs.*] section is decoded into Spec, including the arch/verify
// subsections and the per-OS override (e.g. [specs.mytool.darwin]).
func TestUserConfigSpecs(t *testing.T) {
	data := []byte(`
[apps.mytool]
use = "mytool"
version = "latest"

[specs.mytool]
backend = "github"
repo = "owner/mytool"
asset = "mytool-{{version}}-{{rust_target}}.tar.gz"
archive = "tar.gz"
extract = "mytool{{ext}}"

[specs.mytool.arch]
amd64 = "x86_64"
arm64 = "aarch64"

[specs.mytool.verify]
sha256_asset = "{{asset}}.sha256"

[specs.mytool.darwin]
strip_components = 1
`)
	var raw userConfigRaw
	if err := toml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	specs, err := parseSpecsFromRaw(raw.Specs)
	if err != nil {
		t.Fatal(err)
	}

	s, ok := specs["mytool"]
	if !ok {
		t.Fatal("mytool spec not found")
	}
	if s.Backend != "github" {
		t.Errorf("Backend = %q, want github", s.Backend)
	}
	if s.Repo != "owner/mytool" {
		t.Errorf("Repo = %q, want owner/mytool", s.Repo)
	}
	if s.Arch["amd64"] != "x86_64" {
		t.Errorf("Arch[amd64] = %q, want x86_64", s.Arch["amd64"])
	}
	if s.Verify.SHA256Asset != "{{asset}}.sha256" {
		t.Errorf("Verify.SHA256Asset = %q, want {{asset}}.sha256", s.Verify.SHA256Asset)
	}
	darwin, ok := s.OSOverrides["darwin"]
	if !ok {
		t.Fatal("darwin override not found")
	}
	if darwin.StripComponents == nil || *darwin.StripComponents != 1 {
		t.Errorf("darwin StripComponents = %v, want 1", darwin.StripComponents)
	}
}

// TestMergeUserSpecsOverride verifies that user recipes are added to the
// embedded ones and, when names collide, override them (last-write-wins).
func TestMergeUserSpecsOverride(t *testing.T) {
	embedded := map[string]Spec{
		"ripgrep": {Backend: "github", Repo: "BurntSushi/ripgrep"},
	}
	user := &Config{
		Specs: map[string]Spec{
			"mytool":  {Backend: "github", Repo: "owner/mytool"},
			"ripgrep": {Backend: "url", Source: "https://example.com/rg"},
		},
	}

	cfg, err := Merge(embedded, user)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := cfg.Specs["mytool"]; !ok {
		t.Error("user spec mytool not merged")
	}
	rg := cfg.Specs["ripgrep"]
	if rg.Backend != "url" || rg.Source != "https://example.com/rg" {
		t.Errorf("ripgrep = %+v, want user override (backend url)", rg)
	}
}
