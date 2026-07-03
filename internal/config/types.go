package config

import "strings"

// PlatformOverride contains the fields of a spec that can be overridden per-OS.
type PlatformOverride struct {
	StripComponents *int   `toml:"strip_components"`
	Subdir          string `toml:"subdir"`
	Extract         string `toml:"extract"`
	Chmod           string `toml:"chmod"`
	Source          string `toml:"source"`
	Asset           string `toml:"asset"`
	Archive         string `toml:"archive"`
}

// Binary is an executable to extract from a multi-binary archive.
// From is the (templated) basename of the file inside the archive; To, if
// empty, defaults to From's basename.
type Binary struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

// Spec is a registry spec: describes how to download and install a tool.
type Spec struct {
	Backend string `toml:"backend"`
	Repo    string `toml:"repo"`
	Asset   string `toml:"asset"`
	Source  string `toml:"source"`
	// Tag is the template of the release tag for pinned versions, for repos
	// whose tags are not "v" + version (e.g. "bun-v{{version}}").
	// Empty = default behavior ("v" + version).
	Tag string `toml:"tag"`
	// LatestStrategy explicitly selects the strategy used to resolve "latest";
	// when set it takes precedence over the backend (e.g. "arch-linux").
	LatestStrategy string `toml:"latest_strategy"`
	// ArchPkg is the package name in the official Arch repos (strategy "arch-linux").
	ArchPkg string `toml:"arch_pkg"`
	// DefaultVersion is the version used as "latest" when the backend/strategy
	// cannot resolve "latest" (e.g. backend "url"). Ignored if "latest" is
	// resolvable or if the app pins an explicit version.
	DefaultVersion string `toml:"default_version"`
	Archive        string `toml:"archive"`
	Extract        string `toml:"extract"`
	// Binaries lists multiple executables to extract from the archive and
	// install into dest (interpreted as a bin directory). Mutually exclusive with Extract.
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
	// Platforms lists the supported platforms in paq's canonical vocabulary
	// (e.g. "linux/amd64" or "linux"). Empty = no restriction (all allowed).
	// The arch is optional: "linux" matches all arches.
	Platforms []string `toml:"platforms"`
	// OSOverrides contains per-OS field overrides (e.g. [jdk.darwin]).
	OSOverrides map[string]PlatformOverride `toml:"-"`
	// Origin records where the definition came from (OriginEmbedded,
	// OriginRegistry or OriginUser). Set at load time, not part of the TOML format.
	Origin string `toml:"-"`
}

// Spec origins, in ascending precedence order.
const (
	OriginEmbedded = "embedded" // bundled in the binary
	OriginRegistry = "registry" // external registry snapshot (paq registry update)
	OriginUser     = "user"     // [specs.*] in the user manifest
)

// SupportsPlatform reports whether the spec supports the os/arch pair (in
// paq's canonical vocabulary). Empty Platforms = all allowed. An entry
// without an arch ("linux") matches all arches of that OS; "linux/amd64" only that one.
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

// ApplyOSOverride applies the per-OS override if present, returning a modified copy.
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

// VerifyConfig configures integrity and signature verification.
type VerifyConfig struct {
	SHA256      string         `toml:"sha256"`
	SHA256Asset string         `toml:"sha256_asset"`
	SHA512      string         `toml:"sha512"`
	SHA512Asset string         `toml:"sha512_asset"`
	Minisign    MinisignConfig `toml:"minisign"`
}

// MinisignConfig configures minisign signature verification.
type MinisignConfig struct {
	PublicKey   string `toml:"public_key"`
	SignedAsset string `toml:"signed_asset"`
}

// Enabled indicates whether the spec configures at least one integrity or
// signature check. Used to warn the user when a tool is installed with no verification.
func (v VerifyConfig) Enabled() bool {
	return v.SHA256 != "" || v.SHA256Asset != "" ||
		v.SHA512 != "" || v.SHA512Asset != "" ||
		(v.Minisign.PublicKey != "" && v.Minisign.SignedAsset != "")
}

// AppEntry is an app's configuration in the user manifest (~/.config/paq/config.toml).
type AppEntry struct {
	Use     string            `toml:"use"`
	Version string            `toml:"version"`
	Dest    string            `toml:"dest"`
	OS      map[string]string `toml:"os"`
	Arch    map[string]string `toml:"arch"`
	Env     map[string]string `toml:"env"`
	Chmod   string            `toml:"chmod"`
}

// Defaults collects the user-configurable default values in the manifest
// ([defaults] section). Bin and Opt are the base directories used to derive
// dest when an app doesn't specify one. Empty → falls back to the built-in
// defaults (see DefaultDestRoots).
type Defaults struct {
	Bin string `toml:"bin"`
	Opt string `toml:"opt"`
	// CheckUpdates controls the daily "new paq version available" notice.
	// Nil (unset) means enabled; only an explicit `check_updates = false`
	// disables it.
	CheckUpdates *bool `toml:"check_updates"`
}

// RegistrySettings configures a custom source for "paq registry update"
// ([registry] section in the manifest). Setting URL requires PublicKey: the
// user is explicitly replacing the default trust anchor, so verification stays
// mandatory. Empty = use the default source (paq release assets).
type RegistrySettings struct {
	URL       string `toml:"url"`
	PublicKey string `toml:"public_key"`
}

// Config is the fully merged configuration (registry + user manifest).
type Config struct {
	Specs map[string]Spec
	Apps  map[string]AppEntry
	// Defaults are the user-configurable defaults ([defaults] section).
	Defaults Defaults
	// Registry configures the external registry source ([registry] section).
	Registry RegistrySettings
	// GlobalTemplates contains the global meta-templates from templates.toml.
	GlobalTemplates map[string]string
	// GlobalTemplatesOS contains the per-OS meta-templates from templates.toml.
	GlobalTemplatesOS map[string]map[string]string
}
