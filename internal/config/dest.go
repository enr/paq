package config

import "runtime"

// DefaultDest deriva una destinazione di default dalla spec, usata sia da
// `paq import` (per generare il campo dest) sia dalla pipeline di install
// quando un'app non specifica `dest`:
//   - install di un singolo binario (Extract valorizzato) → <bin>/<binario>
//   - install di più binari (Binaries valorizzato)        → <bin> (directory)
//   - install di una directory                            → <opt>/<nome app>
//
// Per i binari si riusa il template Extract (es. "rg{{ext}}") così il path resta
// cross-platform (l'estensione viene risolta in fase di install).
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

// DefaultDestRoots ritorna le directory base (bin, opt) per le destinazioni di
// default. Se l'utente le ha configurate in [defaults] (Bin/Opt) quelle hanno
// la precedenza; altrimenti si usano i default built-in idiomatici per l'OS:
//   - Unix:    ~/.local/bin, ~/.local/opt
//   - Windows: ~/AppData/Local/paq/{bin,opt}, che si risolve in %LOCALAPPDATA%\paq,
//     la stessa radice già usata da paq per lo state. Si usa il prefisso "~/"
//     (espanso in fase di install) per mantenere il path portabile.
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
