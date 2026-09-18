package config

import "strings"

// PlatformOverride contains the fields of a spec that can be overridden
// per-OS ([x.<os>]) or per OS/arch pair ([x.<os>.<arch>]).
type PlatformOverride struct {
	StripComponents *int   `toml:"strip_components"`
	Subdir          string `toml:"subdir"`
	Extract         string `toml:"extract"`
	Chmod           string `toml:"chmod"`
	Source          string `toml:"source"`
	Asset           string `toml:"asset"`
	Archive         string `toml:"archive"`
	// ArchOverrides contains the per-arch sections nested in a per-OS one
	// (e.g. [micro.darwin.amd64]), keyed by canonical arch. Applied after the
	// OS-level fields. Set at load time, not part of the TOML decoding.
	ArchOverrides map[string]PlatformOverride `toml:"-"`
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
	// LatestURL is the JSON document holding the version (strategy "json"). It
	// is fetched before the platform variables exist, so it is not templated.
	LatestURL string `toml:"latest_url"`
	// LatestJSON selects the version inside that document, as a dot-separated
	// path (e.g. "productVersion", "channels.stable.version").
	LatestJSON string `toml:"latest_json"`
	// DefaultVersion is the version used as "latest" when the backend/strategy
	// cannot resolve "latest" (e.g. backend "url"). Ignored if "latest" is
	// resolvable or if the app pins an explicit version.
	DefaultVersion string `toml:"default_version"`
	// MinimumReleaseAge overrides, for this spec, the minimum age a release
	// must have before "latest" resolves to it (e.g. "7d", "6mo", "1y").
	// Empty = use the user's global default ([defaults] minimum_release_age),
	// or the built-in default (see version.DefaultMinimumReleaseAge) if that's
	// unset too. Only enforced when "latest" resolves via the "github" backend.
	MinimumReleaseAge string `toml:"minimum_release_age"`
	Archive           string `toml:"archive"`
	Extract           string `toml:"extract"`
	// Binaries lists multiple executables to extract from the archive and
	// install into dest (interpreted as a bin directory). Mutually exclusive with Extract.
	Binaries        []Binary          `toml:"binaries"`
	Subdir          string            `toml:"subdir"`
	StripComponents int               `toml:"strip_components"`
	Chmod           string            `toml:"chmod"`
	OS              map[string]string `toml:"os"`
	Arch            map[string]string `toml:"arch"`
	Env             map[string]string `toml:"env"`
	// EnvArch overrides {{env}} per arch (keyed by canonical arch, e.g. amd64).
	// Takes precedence over Env for the matching arch. Needed when a tool ships
	// different C environments per arch (e.g. ripgrep: musl on x86_64, gnu on aarch64).
	//
	// Limitation: unlike Env (keyed on "gnu", empty outside linux, so naturally
	// linux-scoped) this is keyed on arch alone and applies on every OS. It is
	// harmless today only because {{env}} is consumed solely by the linux
	// rust_target template. A joint (os,arch) dependency belongs in the
	// [x.<os>.<arch>] override blocks (see PlatformOverride.ArchOverrides),
	// not in this field.
	EnvArch     map[string]string            `toml:"env_arch"`
	Templates   map[string]string            `toml:"templates"`
	TemplatesOS map[string]map[string]string `toml:"templates_os"`
	Verify      VerifyConfig                 `toml:"verify"`
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

// ApplyPlatformOverride applies the per-OS override ([x.<os>]) and then the
// per-OS/arch one ([x.<os>.<arch>]) if present, returning a modified copy.
// os and arch are the canonical values, before any [x.os] / [x.arch] remapping.
func (r Spec) ApplyPlatformOverride(os, arch string) Spec {
	ov, ok := r.OSOverrides[os]
	if !ok {
		return r
	}
	r = r.applyOverride(ov)
	if archOv, ok := ov.ArchOverrides[arch]; ok {
		r = r.applyOverride(archOv)
	}
	return r
}

// applyOverride applies a single override block, returning a modified copy.
func (r Spec) applyOverride(ov PlatformOverride) Spec {
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
	SHA256      string `toml:"sha256"`
	SHA256Asset string `toml:"sha256_asset"`
	// SHA256URL is the absolute (templated) URL of the checksum document, for
	// projects that do not publish it next to the artifact. Mutually exclusive
	// with SHA256Asset, which is instead resolved as a sibling of the asset.
	SHA256URL string `toml:"sha256_url"`
	// SHA256JSON selects the hash inside a JSON checksum document, as a
	// dot-separated path (e.g. "sha256hash", "assets.0.digest"). When set, the
	// document is parsed as JSON instead of as a checksum file; it applies to
	// both SHA256Asset and SHA256URL.
	SHA256JSON  string         `toml:"sha256_json"`
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
	return v.SHA256 != "" || v.SHA256Asset != "" || v.SHA256URL != "" ||
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

// TracksLatest reports whether the app entry follows the newest upstream
// version: an explicit "latest", or an omitted version when the spec has
// no default_version to pin to. Must stay in sync with the pipeline's
// version-resolution switch (internal/install/pipeline.go).
func (a AppEntry) TracksLatest(spec Spec) bool {
	if strings.EqualFold(a.Version, "latest") {
		return true
	}
	return a.Version == "" && spec.DefaultVersion == ""
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
	// MinimumReleaseAge is the global default minimum release age for
	// "latest" resolution (e.g. "7d", "6mo", "1y"), overridable per spec via
	// Spec.MinimumReleaseAge. Empty = use the built-in default (see
	// version.DefaultMinimumReleaseAge).
	MinimumReleaseAge string `toml:"minimum_release_age"`
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
	// Lock is the parsed paq.lock.toml, consulted by the install pipeline to
	// reproduce a previously-resolved "latest" version instead of re-resolving
	// it live. Never nil once loaded through Merge; callers built directly
	// (mainly tests) must nil-check before use.
	Lock *Lock
}
