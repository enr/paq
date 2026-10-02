package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withMuslLoaderGlob points musl detection at pattern for the test's duration.
func withMuslLoaderGlob(t *testing.T, pattern string) {
	t.Helper()
	old := muslLoaderGlob
	muslLoaderGlob = pattern
	t.Cleanup(func() { muslLoaderGlob = old })
}

func TestDetect(t *testing.T) {
	// Independent of the host's C library: no musl loader.
	withMuslLoaderGlob(t, filepath.Join(t.TempDir(), "ld-musl-*.so.1"))
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

func TestDetectMusl(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("musl detection only applies on linux")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ld-musl-x86_64.so.1"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	withMuslLoaderGlob(t, filepath.Join(dir, "ld-musl-*.so.1"))
	if env := Detect().Env; env != "musl" {
		t.Errorf("Env with a musl loader = %q, want musl", env)
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
	// key not present: returns the default.
	if got := ApplyMap(m, "riscv64", "riscv64"); got != "riscv64" {
		t.Errorf("ApplyMap riscv64 = %q, want riscv64", got)
	}
	// nil map: returns the default.
	if got := ApplyMap(nil, "amd64", "amd64"); got != "amd64" {
		t.Errorf("ApplyMap nil = %q, want amd64", got)
	}
}
