package config

import "runtime"

// DefaultDest derives a default destination from the spec, used both by
// `paq import` (to generate the dest field) and by the install pipeline
// when an app doesn't specify `dest`:
//   - single-binary install (Extract set)   → <bin>/<binary>
//   - multi-binary install (Binaries set)   → <bin> (directory)
//   - directory install                     → <opt>/<app name>
//
// For binaries, the Extract template (e.g. "rg{{ext}}") is reused so the path
// stays cross-platform (the extension is resolved at install time).
func DefaultDest(spec Spec, key string, d Defaults) string {
	binDir, optDir := DefaultDestRoots(d)
	switch {
	case spec.Extract != "":
		return binDir + "/" + spec.Extract
	case len(spec.Binaries) > 0:
		return binDir
	default:
		return optDir + "/" + key
	}
}

// DefaultDestRoots returns the base directories (bin, opt) for default
// destinations. If the user configured them in [defaults] (Bin/Opt), those
// take precedence; otherwise the OS-idiomatic built-in defaults are used:
//   - Unix:    ~/.local/bin, ~/.local/opt
//   - Windows: ~/AppData/Local/paq/{bin,opt}, which resolves to %LOCALAPPDATA%\paq,
//     the same root paq already uses for state. The "~/" prefix is used
//     (expanded at install time) to keep the path portable.
func DefaultDestRoots(d Defaults) (binDir, optDir string) {
	binDir, optDir = builtinDestRoots()
	if d.Bin != "" {
		binDir = d.Bin
	}
	if d.Opt != "" {
		optDir = d.Opt
	}
	return binDir, optDir
}

func builtinDestRoots() (binDir, optDir string) {
	if runtime.GOOS == "windows" {
		return "~/AppData/Local/paq/bin", "~/AppData/Local/paq/opt"
	}
	return "~/.local/bin", "~/.local/opt"
}
