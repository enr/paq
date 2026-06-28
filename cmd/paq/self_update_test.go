package main

import "testing"

func TestSelfUpdateAssetName(t *testing.T) {
	cases := []struct {
		tag, os, arch string
		want          string
	}{
		{"v0.1.0", "linux", "amd64", "paq-v0.1.0-linux-amd64.zip"},
		{"v1.2.3", "darwin", "arm64", "paq-v1.2.3-darwin-arm64.zip"},
		{"v0.1.0", "windows", "amd64", "paq-v0.1.0-windows-amd64.zip"},
	}
	for _, c := range cases {
		if got := selfUpdateAssetName(c.tag, c.os, c.arch); got != c.want {
			t.Errorf("selfUpdateAssetName(%q, %q, %q) = %q, want %q", c.tag, c.os, c.arch, got, c.want)
		}
	}
}
