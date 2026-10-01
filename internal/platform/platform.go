package platform

import (
	"path/filepath"
	"runtime"
)

// muslLoaderGlob matches the dynamic loader of musl-based systems (Alpine,
// Void musl, ...), e.g. /lib/ld-musl-x86_64.so.1. A variable for tests.
var muslLoaderGlob = "/lib/ld-musl-*.so.1"

// Defaults contains the platform values resolved for the current system.
type Defaults struct {
	OS     string // "linux", "darwin", "windows"
	Arch   string // "amd64", "arm64"
	Vendor string // "unknown" on linux, "apple" on darwin, "pc" on windows
	Env    string // "gnu" on linux ("musl" on musl-based systems), "" elsewhere
	Ext    string // "" on linux/darwin, ".exe" on windows
}

// Detect returns the Defaults for the platform the process is running on.
func Detect() Defaults {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	vendor := "unknown"
	switch goos {
	case "darwin":
		vendor = "apple"
	case "windows":
		vendor = "pc"
	}

	env := ""
	if goos == "linux" {
		env = "gnu"
		if isMusl() {
			env = "musl"
		}
	}

	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}

	return Defaults{
		OS:     goos,
		Arch:   goarch,
		Vendor: vendor,
		Env:    env,
		Ext:    ext,
	}
}

// isMusl reports whether the system's C library is musl, by the presence of
// its dynamic loader.
func isMusl() bool {
	matches, _ := filepath.Glob(muslLoaderGlob)
	return len(matches) > 0
}

// ApplyMap applies an override map (e.g. [x.os] or [x.arch]): if the key
// exists in the map, returns the corresponding value, otherwise returns defaultVal.
func ApplyMap(m map[string]string, key, defaultVal string) string {
	if m == nil {
		return defaultVal
	}
	if v, ok := m[key]; ok {
		return v
	}
	return defaultVal
}
