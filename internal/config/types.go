package config

import "strings"

// PlatformOverride contiene i campi di una spec che possono essere sovrascritti per-OS.
type PlatformOverride struct {
	StripComponents *int   `toml:"strip_components"`
	Subdir          string `toml:"subdir"`
	Extract         string `toml:"extract"`
	Chmod           string `toml:"chmod"`
	Source          string `toml:"source"`
	Asset           string `toml:"asset"`
	Archive         string `toml:"archive"`
}

// Binary è un eseguibile da estrarre da un archivio multi-binary.
// From è il basename (templated) del file dentro l'archivio; To, se vuoto,
// vale come il basename di From.
type Binary struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

// Spec è una spec della registry: descrive come scaricare e installare un tool.
type Spec struct {
	Backend string `toml:"backend"`
	Repo    string `toml:"repo"`
	Asset   string `toml:"asset"`
	Source  string `toml:"source"`
	// LatestStrategy explicitly selects the strategy used to resolve "latest";
	// when set it takes precedence over the backend (e.g. "arch-linux").
	LatestStrategy string `toml:"latest_strategy"`
	// ArchPkg is the package name in the official Arch repos (strategy "arch-linux").
	ArchPkg string `toml:"arch_pkg"`
	// DefaultVersion è la versione usata come "latest" quando il backend/strategia
	// non sa risolvere "latest" (es. backend "url"). Ignorato se "latest" è
	// risolvibile o se l'app pinna una versione esplicita.
	DefaultVersion string `toml:"default_version"`
	Archive        string `toml:"archive"`
	Extract        string `toml:"extract"`
	// Binaries elenca più eseguibili da estrarre dall'archivio e installare in
	// dest (interpretato come directory bin). Mutuamente esclusivo con Extract.
	Binaries        []Binary                     `toml:"binaries"`
	Subdir          string                       `toml:"subdir"`
	StripComponents int                          `toml:"strip_components"`
	Chmod           string                       `toml:"chmod"`
	OS              map[string]string            `toml:"os"`
	Arch            map[string]string            `toml:"arch"`
	Env             map[string]string            `toml:"env"`
	Templates       map[string]string            `toml:"templates"`
	TemplatesOS     map[string]map[string]string `toml:"templates_os"`
	Verify          VerifyConfig                 `toml:"verify"`
	// Platforms elenca le piattaforme supportate in vocabolario canonico paq
	// (es. "linux/amd64" o "linux"). Vuoto = nessuna restrizione (tutte ammesse).
	// L'arch è opzionale: "linux" matcha tutte le arch.
	Platforms []string `toml:"platforms"`
	// OSOverrides contiene override di campi per OS specifico (es. [jdk.darwin])
	OSOverrides map[string]PlatformOverride `toml:"-"`
}

// SupportsPlatform riporta se la spec supporta la coppia os/arch (in
// vocabolario canonico paq). Platforms vuoto = tutte ammesse. Una voce senza
// arch ("linux") matcha tutte le arch di quell'OS; "linux/amd64" solo quella.
func (r Spec) SupportsPlatform(os, arch string) bool {
	if len(r.Platforms) == 0 {
		return true
	}
	for _, p := range r.Platforms {
		if pOS, pArch, ok := strings.Cut(p, "/"); ok {
			if pOS == os && pArch == arch {
				return true
			}
		} else if p == os {
			return true
		}
	}
	return false
}

// ApplyOSOverride applica l'override per-OS se presente, ritornando una copia modificata.
func (r Spec) ApplyOSOverride(os string) Spec {
	ov, ok := r.OSOverrides[os]
	if !ok {
		return r
	}
	if ov.StripComponents != nil {
		r.StripComponents = *ov.StripComponents
	}
	if ov.Subdir != "" {
		r.Subdir = ov.Subdir
	}
	if ov.Extract != "" {
		r.Extract = ov.Extract
	}
	if ov.Chmod != "" {
		r.Chmod = ov.Chmod
	}
	if ov.Source != "" {
		r.Source = ov.Source
	}
	if ov.Asset != "" {
		r.Asset = ov.Asset
	}
	if ov.Archive != "" {
		r.Archive = ov.Archive
	}
	return r
}

// VerifyConfig configura la verifica di integrità e firma.
type VerifyConfig struct {
	SHA256      string         `toml:"sha256"`
	SHA256Asset string         `toml:"sha256_asset"`
	SHA512      string         `toml:"sha512"`
	SHA512Asset string         `toml:"sha512_asset"`
	Minisign    MinisignConfig `toml:"minisign"`
}

// MinisignConfig configura la verifica della firma minisign.
type MinisignConfig struct {
	PublicKey   string `toml:"public_key"`
	SignedAsset string `toml:"signed_asset"`
}

// Enabled indica se la spec configura almeno una verifica di integrità o firma.
// È usato per avvisare l'utente quando un tool viene installato senza alcuna verifica.
func (v VerifyConfig) Enabled() bool {
	return v.SHA256 != "" || v.SHA256Asset != "" ||
		v.SHA512 != "" || v.SHA512Asset != "" ||
		(v.Minisign.PublicKey != "" && v.Minisign.SignedAsset != "")
}

// AppEntry è la configurazione di un'app nel manifest utente (~/.config/paq/config.toml).
type AppEntry struct {
	Use     string            `toml:"use"`
	Version string            `toml:"version"`
	Dest    string            `toml:"dest"`
	OS      map[string]string `toml:"os"`
	Arch    map[string]string `toml:"arch"`
	Env     map[string]string `toml:"env"`
	Chmod   string            `toml:"chmod"`
}

// Defaults raccoglie i valori di default configurabili dall'utente nel manifest
// (sezione [defaults]). Bin e Opt sono le directory base usate per derivare il
// dest quando un'app non lo specifica. Vuoti → si usano i default built-in
// (vedi DefaultDestRoots).
type Defaults struct {
	Bin string `toml:"bin"`
	Opt string `toml:"opt"`
}

// Config è la configurazione completa post-merge (registry + manifest utente).
type Config struct {
	Specs map[string]Spec
	Apps  map[string]AppEntry
	// Defaults sono i default configurabili dall'utente (sezione [defaults]).
	Defaults Defaults
	// GlobalTemplates contiene i meta-template globali da templates.toml
	GlobalTemplates map[string]string
	// GlobalTemplatesOS contiene i meta-template per-OS da templates.toml
	GlobalTemplatesOS map[string]map[string]string
}
