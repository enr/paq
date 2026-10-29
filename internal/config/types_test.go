package config

import "testing"

func TestSpecSupportsPlatform(t *testing.T) {
	tests := []struct {
		name      string
		platforms []string
		os        string
		arch      string
		want      bool
	}{
		{"empty allows all", nil, "linux", "amd64", true},
		{"os-only matches any arch", []string{"linux"}, "linux", "arm64", true},
		{"os-only rejects other os", []string{"linux"}, "darwin", "arm64", false},
		{"os/arch exact match", []string{"linux/amd64"}, "linux", "amd64", true},
		{"os/arch rejects other arch", []string{"linux/amd64"}, "linux", "arm64", false},
		{"mixed entries match os/arch", []string{"linux", "darwin/arm64"}, "darwin", "arm64", true},
		{"mixed entries reject darwin/amd64", []string{"linux", "darwin/arm64"}, "darwin", "amd64", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Spec{Platforms: tt.platforms}
			if got := r.SupportsPlatform(tt.os, tt.arch); got != tt.want {
				t.Errorf("SupportsPlatform(%q, %q) = %v, want %v", tt.os, tt.arch, got, tt.want)
			}
		})
	}
}

func TestAppEntryTracksLatest(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		defaultVersion string
		want           bool
	}{
		{"explicit latest", "latest", "", true},
		{"explicit LATEST case-insensitive", "LATEST", "1.0.0", true},
		{"empty version, no default", "", "", true},
		{"empty version, has default", "", "1.0.0", false},
		{"pinned version", "1.2.3", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := AppEntry{Version: tt.version}
			spec := Spec{DefaultVersion: tt.defaultVersion}
			if got := app.TracksLatest(spec); got != tt.want {
				t.Errorf("TracksLatest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpecAuditTarget(t *testing.T) {
	tests := []struct {
		name   string
		toml   string
		want   AuditSource
		wantOK bool
	}{
		{"audit table", `audit = { source = "nodejs" }`, AuditSource{Source: "nodejs"}, true},
		{"legacy osv table", `osv = { ecosystem = "Go", package = "stdlib" }`,
			AuditSource{Source: "osv", Ecosystem: "Go", Package: "stdlib"}, true},
		{"audit wins over osv", "audit = { source = \"nodejs\" }\nosv = { ecosystem = \"Go\", package = \"stdlib\" }",
			AuditSource{Source: "nodejs"}, true},
		{"npm backend", "backend = \"npm\"\npackage = \"esbuild\"",
			AuditSource{Source: "osv", Ecosystem: "npm", Package: "esbuild"}, true},
		{"none", `backend = "github"`, AuditSource{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs, err := parseSpecFile([]byte("[x]\n" + tt.toml))
			if err != nil {
				t.Fatal(err)
			}
			got, ok := specs["x"].AuditTarget()
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("AuditTarget() = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
