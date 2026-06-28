package main

import (
	"errors"
	"strings"
	"testing"
)

func TestHintFor(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantSub string // sottostringa attesa nel suggerimento
	}{
		{"sha256 mismatch", errors.New("integrity check failed: sha256 mismatch for x"), "corrupted or tampered"},
		{"sha512 mismatch", errors.New("integrity check failed: sha512 mismatch for x"), "corrupted or tampered"},
		{"signature", errors.New("signature verification failed: minisign signature is invalid"), "signature could not be verified"},
		{"manifest", errors.New(`app "rg" not found in manifest`), "~/.config/paq/config.toml"},
		{"registry", errors.New(`spec "rg" not found in registry`), "paq registry"},
		{"unsupported platform", errors.New(`"bat" is not available for linux/arm64 (supported: linux/amd64)`), "no build for your OS/architecture"},
		{"github 404", errors.New("GitHub API returned 404 for https://..."), "GITHUB_TOKEN"},
		{"download 404", errors.New("download https://...: HTTP 404"), "build exists for your platform"},
		{"permission", errors.New("install dir: open /usr/bin/x: permission denied"), "writable"},
		{"generic", errors.New("something unexpected happened"), "--debug"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hintFor(tc.err)
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("hintFor(%q) = %q, want substring %q", tc.err, got, tc.wantSub)
			}
		})
	}
}
