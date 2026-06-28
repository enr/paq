package platform

import "runtime"

// Defaults contiene i valori di piattaforma risolti per il sistema corrente.
type Defaults struct {
	OS     string // "linux", "darwin", "windows"
	Arch   string // "amd64", "arm64"
	Vendor string // "unknown" su linux, "apple" su darwin, "pc" su windows
	Env    string // "gnu" su linux, "" altrove
	Ext    string // "" su linux/darwin, ".exe" su windows
}

// Detect ritorna i Defaults per la piattaforma su cui gira il processo.
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

// ApplyMap applica una mappa di override (es. [x.os] o [x.arch]):
// se la chiave esiste nella mappa, ritorna il valore corrispondente,
// altrimenti ritorna il valore di default.
func ApplyMap(m map[string]string, key, defaultVal string) string {
	if m == nil {
		return defaultVal
	}
	if v, ok := m[key]; ok {
		return v
	}
	return defaultVal
}
