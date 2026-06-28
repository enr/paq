package platform

import (
	"runtime"
	"testing"
)

func TestDetect(t *testing.T) {
	d := Detect()
	if d.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", d.OS, runtime.GOOS)
	}
	if d.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", d.Arch, runtime.GOARCH)
	}
	switch d.OS {
	case "linux":
		if d.Vendor != "unknown" {
			t.Errorf("linux Vendor = %q, want unknown", d.Vendor)
		}
		if d.Env != "gnu" {
			t.Errorf("linux Env = %q, want gnu", d.Env)
		}
		if d.Ext != "" {
			t.Errorf("linux Ext = %q, want empty", d.Ext)
		}
	case "darwin":
		if d.Vendor != "apple" {
			t.Errorf("darwin Vendor = %q, want apple", d.Vendor)
		}
		if d.Env != "" {
			t.Errorf("darwin Env = %q, want empty", d.Env)
		}
		if d.Ext != "" {
			t.Errorf("darwin Ext = %q, want empty", d.Ext)
		}
	case "windows":
		if d.Vendor != "pc" {
			t.Errorf("windows Vendor = %q, want pc", d.Vendor)
		}
		if d.Ext != ".exe" {
			t.Errorf("windows Ext = %q, want .exe", d.Ext)
		}
	}
}

func TestApplyMap(t *testing.T) {
	m := map[string]string{
		"amd64": "x86_64",
		"arm64": "aarch64",
	}

	if got := ApplyMap(m, "amd64", "amd64"); got != "x86_64" {
		t.Errorf("ApplyMap amd64 = %q, want x86_64", got)
	}
	if got := ApplyMap(m, "arm64", "arm64"); got != "aarch64" {
		t.Errorf("ApplyMap arm64 = %q, want aarch64", got)
	}
	// chiave non presente: ritorna il default
	if got := ApplyMap(m, "riscv64", "riscv64"); got != "riscv64" {
		t.Errorf("ApplyMap riscv64 = %q, want riscv64", got)
	}
	// mappa nil: ritorna il default
	if got := ApplyMap(nil, "amd64", "amd64"); got != "amd64" {
		t.Errorf("ApplyMap nil = %q, want amd64", got)
	}
}
