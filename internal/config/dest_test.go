package config

import "testing"

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
