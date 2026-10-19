package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDestBuiltinRoots(t *testing.T) {
	binDir, optDir := DefaultDestRoots(Defaults{})

	// single-binary install: reuses the Extract template under the bin dir.
	fileSpec := Spec{Backend: "github", Extract: "rg{{ext}}"}
	if got, want := DefaultDest(fileSpec, "ripgrep", Defaults{}), binDir+"/rg{{ext}}"; got != want {
		t.Errorf("file dest = %q, want %q", got, want)
	}

	// multi-binary install: the bin dir as a directory.
	multiSpec := Spec{Backend: "github", Binaries: []Binary{{From: "a"}, {From: "b"}}}
	if got := DefaultDest(multiSpec, "tool", Defaults{}); got != binDir {
		t.Errorf("multi dest = %q, want %q", got, binDir)
	}

	// directory install: uses the app name under the opt dir.
	dirSpec := Spec{Backend: "url"}
	if got, want := DefaultDest(dirSpec, "jdk", Defaults{}), optDir+"/jdk"; got != want {
		t.Errorf("dir dest = %q, want %q", got, want)
	}
}

func TestDefaultDestUserDefaults(t *testing.T) {
	d := Defaults{Bin: "~/tools/bin", Opt: "~/tools/opt"}

	if got, want := DefaultDest(Spec{Extract: "rg{{ext}}"}, "rg", d), "~/tools/bin/rg{{ext}}"; got != want {
		t.Errorf("file dest = %q, want %q", got, want)
	}
	if got, want := DefaultDest(Spec{}, "jdk", d), "~/tools/opt/jdk"; got != want {
		t.Errorf("dir dest = %q, want %q", got, want)
	}
}

func TestDefaultDestRootsPartialOverride(t *testing.T) {
	builtinBin, builtinOpt := DefaultDestRoots(Defaults{})

	for _, tc := range []struct {
		name    string
		d       Defaults
		wantBin string
		wantOpt string
	}{
		{"only bin configured", Defaults{Bin: "~/custom/bin"}, "~/custom/bin", builtinOpt},
		{"only opt configured", Defaults{Opt: "~/custom/opt"}, builtinBin, "~/custom/opt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, opt := DefaultDestRoots(tc.d)
			if bin != tc.wantBin {
				t.Errorf("bin = %q, want %q", bin, tc.wantBin)
			}
			if opt != tc.wantOpt {
				t.Errorf("opt = %q, want %q", opt, tc.wantOpt)
			}
		})
	}
}

func TestDefaultDestRootsLocalMode(t *testing.T) {
	saved := ProjectRoot
	t.Cleanup(func() { ProjectRoot = saved })
	ProjectRoot = t.TempDir()

	// Relative, so the dest written into a project manifest is portable.
	if bin, opt := DefaultDestRoots(Defaults{}); bin != ".paq/bin" || opt != ".paq/opt" {
		t.Errorf("roots = %q, %q, want .paq/bin, .paq/opt", bin, opt)
	}
	// User-configured [defaults] still win in local mode.
	if bin, _ := DefaultDestRoots(Defaults{Bin: "tools/bin"}); bin != "tools/bin" {
		t.Errorf("bin = %q, want tools/bin", bin)
	}
}

func TestAbsDest(t *testing.T) {
	saved := ProjectRoot
	t.Cleanup(func() { ProjectRoot = saved })

	abs := filepath.Join(t.TempDir(), "x", "..", "node")
	ProjectRoot = ""
	if got, want := mustAbsDest(t, abs), filepath.Clean(abs); got != want {
		t.Errorf("absolute dest = %q, want %q", got, want)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mustAbsDest(t, ".paq/node"), filepath.Join(cwd, ".paq", "node"); got != want {
		t.Errorf("relative dest (global mode) = %q, want %q (against the working directory)", got, want)
	}

	ProjectRoot = t.TempDir()
	if got, want := mustAbsDest(t, ".paq/node"), filepath.Join(ProjectRoot, ".paq", "node"); got != want {
		t.Errorf("relative dest (local mode) = %q, want %q (against the project root)", got, want)
	}
}

func mustAbsDest(t *testing.T, path string) string {
	t.Helper()
	got, err := AbsDest(path)
	if err != nil {
		t.Fatalf("AbsDest(%q): %v", path, err)
	}
	return got
}
