package template

import "testing"

func linuxAmd64Vars() Vars {
	return Vars{
		OS:           "linux",
		Arch:         "x86_64",
		Vendor:       "unknown",
		Env:          "gnu",
		Ext:          "",
		Version:      "14.1.1",
		VersionMajor: "14",
		VersionMinor: "1",
		VersionPatch: "1",
	}
}

func TestResolveBasic(t *testing.T) {
	v := linuxAmd64Vars()
	got, err := Resolve("{{arch}}-{{vendor}}-{{os}}-{{env}}", v)
	if err != nil {
		t.Fatal(err)
	}
	if got != "x86_64-unknown-linux-gnu" {
		t.Errorf("got %q, want x86_64-unknown-linux-gnu", got)
	}
}

func TestResolveExt(t *testing.T) {
	v := linuxAmd64Vars()
	got, err := Resolve("rg{{ext}}", v)
	if err != nil {
		t.Fatal(err)
	}
	if got != "rg" {
		t.Errorf("linux: got %q, want rg", got)
	}

	vWin := v
	vWin.Ext = ".exe"
	got, err = Resolve("rg{{ext}}", vWin)
	if err != nil {
		t.Fatal(err)
	}
	if got != "rg.exe" {
		t.Errorf("windows: got %q, want rg.exe", got)
	}
}

func TestResolveUnknownPlaceholder(t *testing.T) {
	v := linuxAmd64Vars()
	_, err := Resolve("{{unknown}}", v)
	if err == nil {
		t.Error("expected error for unknown placeholder, got nil")
	}
}

func TestResolveVersion(t *testing.T) {
	v := linuxAmd64Vars()
	got, err := Resolve("jdk-{{version}}_{{os}}-{{arch}}_bin.tar.gz", v)
	if err != nil {
		t.Fatal(err)
	}
	want := "jdk-14.1.1_linux-x86_64_bin.tar.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveVersionBuild(t *testing.T) {
	v := Vars{
		OS:           "linux",
		Arch:         "x64",
		Version:      "21.0.11",
		VersionMajor: "21",
		VersionBuild: "10",
	}
	got, err := Resolve("OpenJDK{{version_major}}U-jdk_{{arch}}_{{os}}_hotspot_{{version}}_{{version_build}}.tar.gz", v)
	if err != nil {
		t.Fatal(err)
	}
	want := "OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpandMetaTemplate(t *testing.T) {
	mt := MetaTemplates{
		"rust_target": "{{arch}}-{{vendor}}-{{os}}-{{env}}",
	}
	osOverrides := map[string]MetaTemplates{
		"darwin": {
			"rust_target": "{{arch}}-{{vendor}}-{{os}}",
		},
	}

	// linux amd64
	v := Vars{
		OS: "linux", Arch: "aarch64", Vendor: "unknown", Env: "gnu",
	}
	expanded, err := Expand(mt, osOverrides, v)
	if err != nil {
		t.Fatal(err)
	}
	if got := expanded.Extra["rust_target"]; got != "aarch64-unknown-linux-gnu" {
		t.Errorf("linux rust_target = %q, want aarch64-unknown-linux-gnu", got)
	}

	// darwin arm64: override che omette env
	vDarwin := Vars{
		OS: "darwin", Arch: "aarch64", Vendor: "apple", Env: "",
	}
	expandedDarwin, err := Expand(mt, osOverrides, vDarwin)
	if err != nil {
		t.Fatal(err)
	}
	if got := expandedDarwin.Extra["rust_target"]; got != "aarch64-apple-darwin" {
		t.Errorf("darwin rust_target = %q, want aarch64-apple-darwin", got)
	}
}

func TestResolveWithExtra(t *testing.T) {
	v := Vars{
		OS:      "linux",
		Version: "14.1.1",
		Extra:   map[string]string{"rust_target": "x86_64-unknown-linux-gnu"},
	}
	got, err := Resolve("ripgrep-{{version}}-{{rust_target}}.tar.gz", v)
	if err != nil {
		t.Fatal(err)
	}
	want := "ripgrep-14.1.1-x86_64-unknown-linux-gnu.tar.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
