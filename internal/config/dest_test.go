package config

import "testing"

func TestDefaultDestBuiltinRoots(t *testing.T) {
	binDir, optDir := DefaultDestRoots(Defaults{})

	// install di un singolo binario: riusa il template Extract sotto la bin dir.
	fileSpec := Spec{Backend: "github", Extract: "rg{{ext}}"}
	if got, want := DefaultDest(fileSpec, "ripgrep", Defaults{}), binDir+"/rg{{ext}}"; got != want {
		t.Errorf("file dest = %q, want %q", got, want)
	}

	// install multi-binario: la bin dir come directory.
	multiSpec := Spec{Backend: "github", Binaries: []Binary{{From: "a"}, {From: "b"}}}
	if got := DefaultDest(multiSpec, "tool", Defaults{}); got != binDir {
		t.Errorf("multi dest = %q, want %q", got, binDir)
	}

	// install di una directory: usa il nome dell'app sotto la opt dir.
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
	// Solo bin configurato: opt resta sul built-in.
	builtinBin, builtinOpt := DefaultDestRoots(Defaults{})
	bin, opt := DefaultDestRoots(Defaults{Bin: "~/custom/bin"})
	if bin != "~/custom/bin" {
		t.Errorf("bin = %q, want ~/custom/bin", bin)
	}
	if opt != builtinOpt {
		t.Errorf("opt = %q, want built-in %q", opt, builtinOpt)
	}

	// Solo opt configurato: bin resta sul built-in.
	bin2, opt2 := DefaultDestRoots(Defaults{Opt: "~/custom/opt"})
	if opt2 != "~/custom/opt" {
		t.Errorf("opt = %q, want ~/custom/opt", opt2)
	}
	if bin2 != builtinBin {
		t.Errorf("bin = %q, want built-in %q", bin2, builtinBin)
	}
}
