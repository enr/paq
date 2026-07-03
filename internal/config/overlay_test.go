package config

import (
	"testing"
	"testing/fstest"
)

// snapshotFS builds an in-memory external registry snapshot.
func snapshotFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys["registry/"+name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fsys
}

// embeddedFixture returns specs/templates as loadConfig would load them from
// the embedded registry.
func embeddedFixture(t *testing.T) (map[string]Spec, map[string]string, map[string]map[string]string) {
	t.Helper()
	base := snapshotFS(map[string]string{
		"ripgrep.toml": `
[ripgrep]
backend = "github"
repo = "BurntSushi/ripgrep"
archive = "tar.gz"

[ripgrep.arch]
amd64 = "x86_64"

[ripgrep.verify]
sha256_asset = "{{asset}}.sha256"

[ripgrep.windows]
archive = "zip"
`,
		"zipp.toml": `
[zipp]
backend = "github"
repo = "enr/zipp"
binaries = [ { from = "zipts{{ext}}" }, { from = "zipls{{ext}}" } ]
`,
		"templates.toml": `
[templates]
rust_target = "{{arch}}-unknown-linux-gnu"
keep_me = "embedded-only"

[templates.darwin]
rust_target = "{{arch}}-apple-darwin"
`,
	})
	specs, err := LoadEmbeddedRegistry(base)
	if err != nil {
		t.Fatal(err)
	}
	global, globalOS, err := LoadGlobalTemplates(base)
	if err != nil {
		t.Fatal(err)
	}
	return specs, global, globalOS
}

// TestOverlayRegistryMatrix exercises the three-level merge
// (embedded < external snapshot < user [specs.*]) name collisions and origins.
func TestOverlayRegistryMatrix(t *testing.T) {
	specs, global, globalOS := embeddedFixture(t)

	// External snapshot: overrides ripgrep, adds newtool, overlays templates.
	snap := snapshotFS(map[string]string{
		"ripgrep.toml": `
[ripgrep]
backend = "github"
repo = "BurntSushi/ripgrep"
archive = "tar.gz"
asset = "rg-{{version}}.tar.gz"

[ripgrep.arch]
amd64 = "x86_64"
arm64 = "aarch64"

[ripgrep.verify.minisign]
public_key = "RWTkey"
signed_asset = "{{asset}}.minisig"

[ripgrep.windows]
archive = "zip"
asset = "rg-{{version}}.zip"
`,
		"newtool.toml": `
[newtool]
backend = "url"
source = "https://example.com/newtool-{{version}}.tar.gz"
`,
		"templates.toml": `
[templates]
rust_target = "{{arch}}-unknown-linux-musl"
snap_only = "from-snapshot"

[templates.darwin]
snap_only = "darwin-snap"
`,
	})
	if err := OverlayRegistry(specs, global, globalOS, snap); err != nil {
		t.Fatal(err)
	}

	// User manifest: overrides ripgrep again, adds mytool.
	user := &Config{
		Specs: map[string]Spec{
			"ripgrep": {Backend: "url", Source: "https://example.com/rg", Origin: OriginUser},
			"mytool":  {Backend: "github", Repo: "owner/mytool", Origin: OriginUser},
		},
	}
	cfg, err := Merge(specs, user)
	if err != nil {
		t.Fatal(err)
	}

	// Collision on all three levels: user wins.
	rg := cfg.Specs["ripgrep"]
	if rg.Backend != "url" || rg.Source != "https://example.com/rg" {
		t.Errorf("ripgrep = %+v, want user override", rg)
	}
	if rg.Origin != OriginUser {
		t.Errorf("ripgrep.Origin = %q, want %q", rg.Origin, OriginUser)
	}

	// Non-colliding specs from every level survive with the right origin.
	for name, wantOrigin := range map[string]string{
		"zipp":    OriginEmbedded,
		"newtool": OriginRegistry,
		"mytool":  OriginUser,
	} {
		s, ok := cfg.Specs[name]
		if !ok {
			t.Fatalf("spec %s missing after merge", name)
		}
		if s.Origin != wantOrigin {
			t.Errorf("%s.Origin = %q, want %q", name, s.Origin, wantOrigin)
		}
	}

	// zipp (embedded only) keeps its complex fields.
	if got := len(cfg.Specs["zipp"].Binaries); got != 2 {
		t.Errorf("zipp.Binaries len = %d, want 2", got)
	}

	// Template overlay is per key: snapshot wins on collision, embedded-only
	// and snapshot-only keys both survive.
	if global["rust_target"] != "{{arch}}-unknown-linux-musl" {
		t.Errorf("rust_target = %q, want snapshot value", global["rust_target"])
	}
	if global["keep_me"] != "embedded-only" {
		t.Errorf("keep_me = %q, want embedded value", global["keep_me"])
	}
	if global["snap_only"] != "from-snapshot" {
		t.Errorf("snap_only = %q, want snapshot value", global["snap_only"])
	}
	// Per-OS templates merge per key too: the embedded darwin override
	// survives, the snapshot-only darwin key is added.
	if globalOS["darwin"]["rust_target"] != "{{arch}}-apple-darwin" {
		t.Errorf("rust_target darwin override = %q", globalOS["darwin"]["rust_target"])
	}
	if globalOS["darwin"]["snap_only"] != "darwin-snap" {
		t.Errorf("snap_only darwin override = %q, want darwin-snap", globalOS["darwin"]["snap_only"])
	}
}

// TestOverlayRegistryReplacesWholeSpec verifies that a colliding snapshot
// spec replaces the embedded one entirely (no field-by-field merge).
func TestOverlayRegistryReplacesWholeSpec(t *testing.T) {
	specs, global, globalOS := embeddedFixture(t)

	snap := snapshotFS(map[string]string{
		// ripgrep redefined without verify and without windows override.
		"ripgrep.toml": `
[ripgrep]
backend = "github"
repo = "BurntSushi/ripgrep"
archive = "tar.gz"

[ripgrep.arch]
amd64 = "x86_64"
arm64 = "aarch64"
`,
	})
	if err := OverlayRegistry(specs, global, globalOS, snap); err != nil {
		t.Fatal(err)
	}

	rg := specs["ripgrep"]
	if rg.Origin != OriginRegistry {
		t.Errorf("ripgrep.Origin = %q, want %q", rg.Origin, OriginRegistry)
	}
	if rg.Arch["arm64"] != "aarch64" {
		t.Errorf("ripgrep.Arch[arm64] = %q, want aarch64", rg.Arch["arm64"])
	}
	// Fields absent in the snapshot spec must not leak in from the embedded one.
	if rg.Verify.Enabled() {
		t.Error("ripgrep.Verify should be empty after whole-spec replacement")
	}
	if _, ok := rg.OSOverrides["windows"]; ok {
		t.Error("ripgrep windows override should be gone after whole-spec replacement")
	}
	// Templates untouched: snapshot has no templates.toml.
	if global["rust_target"] != "{{arch}}-unknown-linux-gnu" {
		t.Errorf("rust_target = %q, want embedded value", global["rust_target"])
	}
}

// TestOverlayRegistryCorruptSnapshot verifies that a broken snapshot leaves
// specs and templates exactly as embedded-only.
func TestOverlayRegistryCorruptSnapshot(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"invalid recipe toml": snapshotFS(map[string]string{
			"broken.toml": "not [valid toml",
		}),
		"invalid templates.toml": snapshotFS(map[string]string{
			"newtool.toml":   "[newtool]\nbackend = \"github\"\n",
			"templates.toml": "not [valid toml",
		}),
		"missing registry dir": {},
	}

	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			specs, global, globalOS := embeddedFixture(t)
			if err := OverlayRegistry(specs, global, globalOS, snap); err == nil {
				t.Fatal("OverlayRegistry should fail on a corrupt snapshot")
			}
			// Nothing was applied: same specs, all embedded, templates intact.
			if len(specs) != 2 {
				t.Errorf("specs len = %d, want 2", len(specs))
			}
			for name, s := range specs {
				if s.Origin != OriginEmbedded {
					t.Errorf("%s.Origin = %q, want %q", name, s.Origin, OriginEmbedded)
				}
			}
			if _, ok := specs["newtool"]; ok {
				t.Error("newtool from corrupt snapshot must not be applied")
			}
			if global["rust_target"] != "{{arch}}-unknown-linux-gnu" {
				t.Errorf("rust_target = %q, want embedded value", global["rust_target"])
			}
		})
	}
}

// TestOverlayRegistryWithoutTemplates verifies that a snapshot without
// templates.toml applies its specs and keeps the embedded templates.
func TestOverlayRegistryWithoutTemplates(t *testing.T) {
	specs, global, globalOS := embeddedFixture(t)

	snap := snapshotFS(map[string]string{
		"newtool.toml": "[newtool]\nbackend = \"github\"\nrepo = \"owner/newtool\"\n",
	})
	if err := OverlayRegistry(specs, global, globalOS, snap); err != nil {
		t.Fatal(err)
	}

	if _, ok := specs["newtool"]; !ok {
		t.Error("newtool not applied")
	}
	if global["rust_target"] != "{{arch}}-unknown-linux-gnu" {
		t.Errorf("rust_target = %q, want embedded value", global["rust_target"])
	}
	if globalOS["darwin"]["rust_target"] != "{{arch}}-apple-darwin" {
		t.Errorf("rust_target darwin = %q, want embedded value", globalOS["darwin"]["rust_target"])
	}
}
