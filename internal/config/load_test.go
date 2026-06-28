package config

import (
	"strings"
	"testing"

	"github.com/enr/paq/embedded"
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

	// zipp deve usare la modalità multi-binary
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

// TestRunpSpec verifica la spec runp (backend github, asset zip multi-piattaforma).
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
	// Una piattaforma non distribuita non deve essere supportata.
	if runp.SupportsPlatform("darwin", "amd64") {
		t.Error("runp should not support darwin/amd64")
	}
}

// TestWindowsArchiveOverride verifica che le ricette che su Windows distribuiscono
// uno zip (bat/ripgrep/delta/hugo) lo selezionino via la sezione [<name>.windows].
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
		// Default (non-Windows): resta tar.gz.
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
		// L'override non deve mutare la spec originale.
		if r.Archive != "tar.gz" {
			t.Errorf("%s Archive mutated to %q after ApplyOSOverride", name, r.Archive)
		}
	}
}

// TestParseSpecBinariesRename verifica il round-trip del parsing di una spec
// con binaries che usa il campo opzionale "to".
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
