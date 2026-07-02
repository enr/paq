package platform

import "runtime"

// Defaults contains the platform values resolved for the current system.
type Defaults struct {
	OS     string // "linux", "darwin", "windows"
	Arch   string // "amd64", "arm64"
	Vendor string // "unknown" on linux, "apple" on darwin, "pc" on windows
	Env    string // "gnu" on linux, "" elsewhere
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
