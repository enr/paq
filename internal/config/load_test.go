package config

import (
	"strings"
	"testing"

	"github.com/enr/paq/embedded"
	"github.com/enr/paq/internal/platform"
	"github.com/enr/paq/internal/template"
	"github.com/pelletier/go-toml/v2"
)

// TestLoadEmbeddedRegistry verifies that the embedded registry parses into a
// non-empty set of specs. This is deliberately the only assertion here: it
// catches real registry breakage (a syntax error, an empty registry) and
// never needs updating when a routine data commit (a version bump, a new
// recipe) touches the registry but not the loader. Loader *behaviour* — per-OS
// overrides, per-OS-arch overrides, arch mapping, multi-binary parsing,
// template expansion — is covered on synthetic fixtures below and in
// overlay_test.go, so it survives registry data changes untouched.
func TestLoadEmbeddedRegistry(t *testing.T) {
	specs, err := LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) == 0 {
		t.Fatal("embedded registry parsed to zero specs")
	}
}

// TestEmbeddedRegistrySpecsAreStructurallyValid is a structural validity pass
// over every embedded spec: catches real registry-authoring mistakes (a
// backend typo, a github spec with no repo, extract+binaries both set) without
// ever asserting on any spec's actual data, so it never needs updating for a
// routine registry commit.
func TestEmbeddedRegistrySpecsAreStructurallyValid(t *testing.T) {
	specs, err := LoadEmbeddedRegistry(embedded.RegistryFS)
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range specs {
		if s.Backend == "" {
			t.Errorf("%s: no backend", name)
			continue
		}
		switch s.Backend {
		case "github":
			if s.Repo == "" {
				t.Errorf("%s: github backend with no repo", name)
			}
		case "url":
			if s.Source == "" {
				t.Errorf("%s: url backend with no source", name)
			}
		default:
			t.Errorf("%s: unknown backend %q", name, s.Backend)
		}
		if s.Extract != "" && len(s.Binaries) > 0 {
			t.Errorf("%s: sets both extract and binaries (mutually exclusive)", name)
		}
	}
}

// TestRunpSpec verifies, on a synthetic github-backend spec with a
// multi-platform zip asset, that SupportsPlatform matches exactly the
// platforms listed — not tied to any real recipe's data, so retiring or
// changing a registry recipe cannot break it.
func TestRunpSpec(t *testing.T) {
	data := []byte(`
[tool]
backend = "github"
repo = "owner/tool"
asset = "tool-{{version}}_{{os}}_{{arch}}.zip"
archive = "zip"
extract = "tool{{ext}}"
platforms = ["linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64"]

[tool.verify]
sha256_asset = "tool-{{version}}_checksums.txt"
`)
	specs, err := parseSpecFile(data)
	if err != nil {
		t.Fatal(err)
	}
	tool := specs["tool"]

	wantPlatforms := []string{"linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64"}
	for _, p := range wantPlatforms {
		os, arch, _ := strings.Cut(p, "/")
		if !tool.SupportsPlatform(os, arch) {
			t.Errorf("tool should support %s", p)
		}
	}
	// A platform not listed must not be supported.
	if tool.SupportsPlatform("darwin", "amd64") {
		t.Error("tool should not support darwin/amd64")
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
		win := r.ApplyPlatformOverride("windows", "amd64")
		if win.Archive != "zip" {
			t.Errorf("%s windows Archive = %q, want zip", name, win.Archive)
		}
		if got := win.Asset[len(win.Asset)-4:]; got != ".zip" {
			t.Errorf("%s windows Asset = %q, want .zip suffix", name, win.Asset)
		}
		// The override must not mutate the original spec.
		if r.Archive != "tar.gz" {
			t.Errorf("%s Archive mutated to %q after ApplyPlatformOverride", name, r.Archive)
		}
	}
}

// TestMicroSpec verifies, on a synthetic spec shaped like a recipe whose
// asset name depends on the (os, arch) pair, that a per-OS override combined
// with a nested per-OS-arch sub-section (a legacy asset name for one
// specific pair, here darwin/amd64) resolves correctly through
// ApplyPlatformOverride + template.Resolve. Built from a fabricated spec
// rather than a real recipe, so a routine registry data change cannot break it.
func TestMicroSpec(t *testing.T) {
	data := []byte(`
[tool]
backend = "url"
source = "https://example.invalid/tool-{{version}}-{{os}}-{{arch}}"
asset = "tool-{{version}}-linux64.tar.gz"
archive = "tar.gz"

[tool.verify]
sha256_asset = "{{asset}}.sha"

[tool.linux.arm64]
asset = "tool-{{version}}-linux-arm64.tar.gz"

[tool.darwin]
asset = "tool-{{version}}-macos-arm64.tar.gz"

[tool.darwin.amd64]
asset = "tool-{{version}}-osx.tar.gz"

[tool.windows]
archive = "zip"
asset = "tool-{{version}}-win64.zip"

[tool.windows.arm64]
asset = "tool-{{version}}-win-arm64.zip"
`)
	specs, err := parseSpecFile(data)
	if err != nil {
		t.Fatal(err)
	}
	tool := specs["tool"]
	if tool.Verify.SHA256Asset != "{{asset}}.sha" {
		t.Errorf("Verify.SHA256Asset = %q, want {{asset}}.sha", tool.Verify.SHA256Asset)
	}

	cases := []struct {
		os, arch    string
		wantAsset   string
		wantArchive string
	}{
		{"linux", "amd64", "tool-2.0.15-linux64.tar.gz", "tar.gz"},
		{"linux", "arm64", "tool-2.0.15-linux-arm64.tar.gz", "tar.gz"},
		{"darwin", "amd64", "tool-2.0.15-osx.tar.gz", "tar.gz"},
		{"darwin", "arm64", "tool-2.0.15-macos-arm64.tar.gz", "tar.gz"},
		{"windows", "amd64", "tool-2.0.15-win64.zip", "zip"},
		{"windows", "arm64", "tool-2.0.15-win-arm64.zip", "zip"},
	}

	for _, tc := range cases {
		if !tool.SupportsPlatform(tc.os, tc.arch) {
			t.Errorf("tool should support %s/%s", tc.os, tc.arch)
		}
		s := tool.ApplyPlatformOverride(tc.os, tc.arch)
		vars := template.Vars{
			OS:      platform.ApplyMap(s.OS, tc.os, tc.os),
			Arch:    platform.ApplyMap(s.Arch, tc.arch, tc.arch),
			Version: "2.0.15",
		}
		asset, err := template.Resolve(s.Asset, vars)
		if err != nil {
			t.Fatalf("%s/%s: resolve asset: %v", tc.os, tc.arch, err)
		}
		if asset != tc.wantAsset {
			t.Errorf("%s/%s asset = %q, want %q", tc.os, tc.arch, asset, tc.wantAsset)
		}
		if s.Archive != tc.wantArchive {
			t.Errorf("%s/%s archive = %q, want %q", tc.os, tc.arch, s.Archive, tc.wantArchive)
		}
	}
}

// TestVSCodeSpec verifies, on a synthetic spec shaped like a recipe whose
// download URL and checksum URL both embed a meta-template value that
// differs on one OS (Windows needs an "-archive" suffix), that
// ApplyPlatformOverride + template.Expand + template.Resolve produce the
// right URL and archive settings per (os, arch), and that SupportsPlatform
// correctly excludes a platform the spec doesn't list. Built from a
// fabricated spec rather than the real vscode recipe, so a routine registry
// data change cannot break it.
func TestVSCodeSpec(t *testing.T) {
	data := []byte(`
[tool]
backend = "url"
source = "https://update.example.invalid/{{version}}/{{tool_platform}}/stable"
archive = "tar.gz"
strip_components = 1
latest_strategy = "json"
latest_url = "https://update.example.invalid/api/update/linux-x64/stable/latest"
latest_json = "productVersion"
platforms = ["linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"]

[tool.templates]
tool_platform = "{{os}}-{{arch}}"

[tool.templates_os.windows]
tool_platform = "{{os}}-{{arch}}-archive"

[tool.arch]
amd64 = "x64"

[tool.os]
windows = "win32"

[tool.windows]
archive = "zip"
strip_components = 0

[tool.verify]
sha256_url = "https://update.example.invalid/api/update/{{tool_platform}}/stable/latest"
sha256_json = "sha256hash"
`)
	specs, err := parseSpecFile(data)
	if err != nil {
		t.Fatal(err)
	}
	tool := specs["tool"]
	if tool.LatestStrategy != "json" {
		t.Errorf("LatestStrategy = %q, want json", tool.LatestStrategy)
	}
	if !tool.Verify.Enabled() {
		t.Error("tool must configure verification")
	}

	cases := []struct {
		os, arch    string
		wantSource  string
		wantArchive string
		wantStrip   int
	}{
		{"linux", "amd64", "https://update.example.invalid/1.99.0/linux-x64/stable", "tar.gz", 1},
		{"linux", "arm64", "https://update.example.invalid/1.99.0/linux-arm64/stable", "tar.gz", 1},
		{"windows", "amd64", "https://update.example.invalid/1.99.0/win32-x64-archive/stable", "zip", 0},
		{"windows", "arm64", "https://update.example.invalid/1.99.0/win32-arm64-archive/stable", "zip", 0},
	}

	for _, tc := range cases {
		if !tool.SupportsPlatform(tc.os, tc.arch) {
			t.Errorf("tool should support %s/%s", tc.os, tc.arch)
		}
		s := tool.ApplyPlatformOverride(tc.os, tc.arch)
		vars := template.Vars{
			OS:      platform.ApplyMap(s.OS, tc.os, tc.os),
			Arch:    platform.ApplyMap(s.Arch, tc.arch, tc.arch),
			Version: "1.99.0",
		}
		osMT := map[string]template.MetaTemplates{}
		for osName, mt := range s.TemplatesOS {
			osMT[osName] = mt
		}
		vars, err := template.Expand(s.Templates, osMT, tc.os, vars)
		if err != nil {
			t.Fatalf("%s/%s: expand meta-templates: %v", tc.os, tc.arch, err)
		}
		source, err := template.Resolve(s.Source, vars)
		if err != nil {
			t.Fatalf("%s/%s: resolve source: %v", tc.os, tc.arch, err)
		}
		if source != tc.wantSource {
			t.Errorf("%s/%s source = %q, want %q", tc.os, tc.arch, source, tc.wantSource)
		}
		// The checksum URL must name the same platform as the download.
		checksumURL, err := template.Resolve(s.Verify.SHA256URL, vars)
		if err != nil {
			t.Fatalf("%s/%s: resolve sha256_url: %v", tc.os, tc.arch, err)
		}
		if want := "/api/update/" + vars.Extra["tool_platform"] + "/stable/latest"; !strings.HasSuffix(checksumURL, want) {
			t.Errorf("%s/%s checksum URL = %q, want suffix %q", tc.os, tc.arch, checksumURL, want)
		}
		if s.Archive != tc.wantArchive {
			t.Errorf("%s/%s archive = %q, want %q", tc.os, tc.arch, s.Archive, tc.wantArchive)
		}
		if s.StripComponents != tc.wantStrip {
			t.Errorf("%s/%s strip_components = %d, want %d", tc.os, tc.arch, s.StripComponents, tc.wantStrip)
		}
	}

	// A platform not listed in "platforms" must not be supported.
	if tool.SupportsPlatform("darwin", "arm64") {
		t.Error("tool should not support darwin/arm64")
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

[specs.mytool.darwin.amd64]
asset = "mytool-{{version}}-osx.tar.gz"
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
	// The nested per-arch sub-section must not swallow the OS-level fields.
	amd64, ok := darwin.ArchOverrides["amd64"]
	if !ok {
		t.Fatal("darwin/amd64 override not found")
	}
	if amd64.Asset != "mytool-{{version}}-osx.tar.gz" {
		t.Errorf("darwin/amd64 Asset = %q, want mytool-{{version}}-osx.tar.gz", amd64.Asset)
	}
}

// TestUserConfigMinimumReleaseAge verifies that minimum_release_age round-trips
// from TOML at both the global ([defaults]) and per-spec level.
func TestUserConfigMinimumReleaseAge(t *testing.T) {
	data := []byte(`
[defaults]
minimum_release_age = "7d"

[specs.mytool]
backend = "github"
repo = "owner/mytool"
minimum_release_age = "0h"
`)
	var raw userConfigRaw
	if err := toml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Defaults.MinimumReleaseAge != "7d" {
		t.Errorf("Defaults.MinimumReleaseAge = %q, want 7d", raw.Defaults.MinimumReleaseAge)
	}

	specs, err := parseSpecsFromRaw(raw.Specs)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := specs["mytool"]
	if !ok {
		t.Fatal("mytool spec not found")
	}
	if s.MinimumReleaseAge != "0h" {
		t.Errorf("Spec.MinimumReleaseAge = %q, want 0h", s.MinimumReleaseAge)
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
