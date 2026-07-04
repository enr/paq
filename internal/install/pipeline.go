package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enr/paq/internal/archive"
	"github.com/enr/paq/internal/backend"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/download"
	"github.com/enr/paq/internal/platform"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/template"
	"github.com/enr/paq/internal/verify"
	"github.com/enr/paq/internal/version"
)

// Hooks lets the pipeline emit UI events without importing the ui package.
type Hooks struct {
	OnStep  func(msg string)
	OnOK    func(msg string)
	OnFail  func(err error)
	OnInfo  func(msg string)
	OnWarn  func(msg string)
	OnDebug func(msg string)
	// Force bypasses the already-installed check and reinstalls unconditionally.
	Force bool
}

// shownError marks an error as already shown to the user via the OnFail hook,
// so callers don't reprint it a second time.
type shownError struct{ err error }

func (e shownError) Error() string { return e.err.Error() }
func (e shownError) Unwrap() error { return e.err }

// ErrAlreadyShown reports whether err (or an error it wraps) has already been
// shown to the user by the pipeline.
func ErrAlreadyShown(err error) bool {
	var se shownError
	return errors.As(err, &se)
}

// Run executes the complete pipeline to install a single app.
// On error, the outcome is shown only once via the OnFail hook, and the
// returned error is marked as already shown (shownError).
func Run(ctx context.Context, cfg *config.Config, appName string, progress download.ProgressFn, hooks *Hooks) (retErr error) {
	if hooks == nil {
		hooks = &Hooks{}
	}
	// Single point of error presentation: any error return is shown here
	// in red and marked as "already shown".
	defer func() {
		if retErr != nil && hooks.OnFail != nil {
			hooks.OnFail(retErr)
			retErr = shownError{err: retErr}
		}
	}()
	step := func(msg string) {
		if hooks.OnStep != nil {
			hooks.OnStep(msg)
		}
	}
	ok := func(msg string) {
		if hooks.OnOK != nil {
			hooks.OnOK(msg)
		}
	}
	warn := func(msg string) {
		if hooks.OnWarn != nil {
			hooks.OnWarn(msg)
		}
	}
	dbg := func(format string, a ...any) {
		if hooks.OnDebug != nil {
			hooks.OnDebug(fmt.Sprintf(format, a...))
		}
	}

	// 1. Find the app and recipe in the config.
	app, found := cfg.Apps[appName]
	if !found {
		return fmt.Errorf("app %q not found in manifest", appName)
	}

	specName := app.Use
	if specName == "" {
		specName = appName
	}
	spec, found := cfg.Specs[specName]
	if !found {
		return fmt.Errorf("spec %q not found in registry", specName)
	}
	dbg("app=%q spec=%q backend=%q version=%q dest=%q", appName, specName, spec.Backend, app.Version, app.Dest)

	// Reject a broken minisign configuration before any network access.
	// Minisign signs the sha256 checksum file: without sha256_asset there is
	// nothing to verify the signature against, and a half-configured minisign
	// would otherwise be silently skipped while looking enabled.
	if (spec.Verify.Minisign.PublicKey != "") != (spec.Verify.Minisign.SignedAsset != "") {
		return fmt.Errorf("spec %q: verify.minisign requires both public_key and signed_asset", specName)
	}
	if spec.Verify.Minisign.PublicKey != "" && spec.Verify.SHA256Asset == "" {
		return fmt.Errorf("spec %q: verify.minisign requires sha256_asset (the signature is verified against the checksum file)", specName)
	}

	// Reject unsupported platforms immediately, before any network access.
	// The check uses the raw canonical values (plat.OS/plat.Arch), not the
	// tool's post-mapping ones.
	plat := platform.Detect()
	dbg("platform: os=%q arch=%q vendor=%q env=%q ext=%q", plat.OS, plat.Arch, plat.Vendor, plat.Env, plat.Ext)
	if !spec.SupportsPlatform(plat.OS, plat.Arch) {
		return fmt.Errorf("%q is not available for %s/%s (supported: %s)", specName, plat.OS, plat.Arch, strings.Join(spec.Platforms, ", "))
	}

	// Warn if the spec configures no verification: the download cannot be
	// validated (integrity/signature). Verification is the main security
	// feature, so its absence must stay visible.
	if !spec.Verify.Enabled() {
		warn(fmt.Sprintf("%q has no verification configured: integrity and signature cannot be checked", specName))
	}

	// 2. Resolve the version
	//   - version omitted → the spec's default_version; if absent, "latest"
	//   - "latest"        → live resolution via strategy/backend (no fallback)
	//   - "x.y.z"         → explicit pin
	step(fmt.Sprintf("Resolving version for %s...", appName))
	var versionProvider version.Provider
	switch {
	case app.Version == "" && spec.DefaultVersion != "":
		versionProvider = version.PinProvider{Version: spec.DefaultVersion, TagTemplate: spec.Tag}
	case app.Version == "" || strings.EqualFold(app.Version, "latest"):
		versionProvider = version.LatestProvider(version.LatestRequest{
			Strategy: spec.LatestStrategy,
			Backend:  spec.Backend,
			Repo:     spec.Repo,
			Source:   spec.Source,
			ArchPkg:  spec.ArchPkg,
		})
	default:
		versionProvider = version.PinProvider{Version: app.Version, TagTemplate: spec.Tag}
	}

	dbg("version provider: %T (repo=%q)", versionProvider, spec.Repo)
	ver, tag, err := versionProvider.Resolve(ctx)
	if err != nil {
		if errors.Is(err, version.ErrLatestNotImplemented) && spec.DefaultVersion != "" {
			return fmt.Errorf("%q has no \"latest\" strategy: omit the version to use the default %s, or pin an explicit version", specName, spec.DefaultVersion)
		}
		return fmt.Errorf("resolve version: %w", err)
	}
	ok(fmt.Sprintf("Version: %s (tag: %s)", ver, tag))

	// FEATURE-1: skip if already installed (unless --force).
	if !hooks.Force {
		if st, lerr := state.Load(); lerr == nil {
			if _, exists := st.Get(appName, ver); exists {
				ok(fmt.Sprintf("%s %s is already installed (use --force to reinstall)", appName, ver))
				return nil
			}
		}
	}

	// 3. Build the template variables.
	versionMajor, versionMinor, versionPatch := version.Parse(ver)
	versionBuild := version.Build(tag)

	// Apply the spec's per-OS override (e.g. jdk has [jdk.darwin]).
	spec = spec.ApplyOSOverride(plat.OS)

	if spec.Extract != "" && len(spec.Binaries) > 0 {
		return fmt.Errorf("spec %q sets both 'extract' and 'binaries': they are mutually exclusive", specName)
	}

	// Apply the spec's arch/os override (maps [ripgrep.arch]).
	resolvedArch := platform.ApplyMap(spec.Arch, plat.Arch, plat.Arch)
	resolvedOS := platform.ApplyMap(spec.OS, plat.OS, plat.OS)

	// Apply the app manifest override (takes precedence over the spec).
	if app.Arch != nil {
		resolvedArch = platform.ApplyMap(app.Arch, plat.Arch, resolvedArch)
	}
	if app.OS != nil {
		resolvedOS = platform.ApplyMap(app.OS, plat.OS, resolvedOS)
	}
	resolvedEnv := platform.ApplyMap(spec.Env, plat.Env, plat.Env)
	if app.Env != nil {
		resolvedEnv = platform.ApplyMap(app.Env, plat.Env, resolvedEnv)
	}

	vars := template.Vars{
		OS:           resolvedOS,
		Arch:         resolvedArch,
		Vendor:       plat.Vendor,
		Env:          resolvedEnv,
		Ext:          plat.Ext,
		Version:      ver,
		VersionMajor: versionMajor,
		VersionMinor: versionMinor,
		VersionPatch: versionPatch,
		VersionBuild: versionBuild,
	}

	// 4. Expand the meta-templates.
	// Load the embedded templates.toml.
	globalMT, osMT := loadTemplates(cfg, spec)
	vars, err = template.Expand(globalMT, osMT, plat.OS, vars)
	if err != nil {
		return fmt.Errorf("expand meta-templates: %w", err)
	}

	// 5. Resolve dest. If the app doesn't specify one, derive the default from
	// the spec and the user-configured base directories ([defaults]).
	destTemplate := app.Dest
	if destTemplate == "" {
		destTemplate = config.DefaultDest(spec, appName, cfg.Defaults)
	}
	dest, err := template.Resolve(destTemplate, vars)
	if err != nil {
		return fmt.Errorf("resolve dest: %w", err)
	}
	dest = expandHome(dest)

	// 6. Resolve the artifact URL.
	step(fmt.Sprintf("Resolving download URL for %s...", appName))
	var downloadURL string
	switch spec.Backend {
	case "github":
		gb := backend.GitHubBackend{Repo: spec.Repo, Asset: spec.Asset}
		downloadURL, err = gb.Resolve(ctx, tag, vars)
	case "url":
		ub := backend.URLBackend{Source: spec.Source}
		downloadURL, err = ub.Resolve(vars)
	default:
		err = fmt.Errorf("unknown backend: %q", spec.Backend)
	}
	if err != nil {
		return fmt.Errorf("resolve download URL: %w", err)
	}
	ok(fmt.Sprintf("URL: %s", downloadURL))
	dbg("resolved: os=%q arch=%q env=%q dest=%q", resolvedOS, resolvedArch, resolvedEnv, dest)

	// Variables for asset names.
	assetName := filepath.Base(downloadURL)
	if vars.Extra == nil {
		vars.Extra = make(map[string]string)
	}
	// {{asset}} defaults to the URL's basename: useful for the "url" backend
	// (e.g. maven) where there's no explicit asset field.
	vars.Extra["asset"] = assetName
	// Resolve the asset name from the template and add it to vars.Extra so
	// that {{asset}} is available in subsequent templates (e.g. sha256_asset).
	if spec.Asset != "" {
		name, err2 := template.Resolve(spec.Asset, vars)
		if err2 != nil {
			return fmt.Errorf("resolve asset name: %w", err2)
		}
		assetName = name
		vars.Extra["asset"] = name
	}
	dbg("asset name: %q", assetName)

	// resolveAuxURL builds the URL of an auxiliary asset (checksum, signature)
	// in a backend-aware way. For the "github" backend the download URL is the
	// asset's API endpoint (…/releases/assets/{id}), which doesn't contain the
	// file name: the auxiliary asset must therefore be resolved by name via the
	// API. For other backends the URL contains the file name and it's enough
	// to substitute it.
	resolveAuxURL := func(auxName string) (string, error) {
		if spec.Backend == "github" {
			gb := backend.GitHubBackend{Repo: spec.Repo, Asset: auxName}
			return gb.Resolve(ctx, tag, vars)
		}
		return buildAuxURL(downloadURL, assetName, auxName)
	}

	client := download.NewClient()

	// 7. Download checksum/signature files (if configured).
	var checksumPath, checksum512Path, sigPath string
	defer func() {
		if checksumPath != "" {
			os.Remove(checksumPath)
		}
		if checksum512Path != "" {
			os.Remove(checksum512Path)
		}
		if sigPath != "" {
			os.Remove(sigPath)
		}
	}()

	if spec.Verify.SHA256Asset != "" {
		sha256AssetName, err2 := template.Resolve(spec.Verify.SHA256Asset, vars)
		if err2 != nil {
			return fmt.Errorf("resolve sha256_asset: %w", err2)
		}
		checksumURL, err2 := resolveAuxURL(sha256AssetName)
		if err2 != nil {
			return fmt.Errorf("resolve sha256_asset URL: %w", err2)
		}
		step("Downloading checksum file...")
		dbg("sha256 checksum URL: %s", checksumURL)
		checksumPath, err = download.ToTemp(ctx, client, checksumURL, nil)
		if err != nil {
			return fmt.Errorf("download checksum: %w", err)
		}
		dbg("sha256 checksum saved to %s", checksumPath)
	}

	if spec.Verify.SHA512Asset != "" {
		sha512AssetName, err2 := template.Resolve(spec.Verify.SHA512Asset, vars)
		if err2 != nil {
			return fmt.Errorf("resolve sha512_asset: %w", err2)
		}
		checksum512URL, err2 := resolveAuxURL(sha512AssetName)
		if err2 != nil {
			return fmt.Errorf("resolve sha512_asset URL: %w", err2)
		}
		step("Downloading checksum file...")
		dbg("sha512 checksum URL: %s", checksum512URL)
		checksum512Path, err = download.ToTemp(ctx, client, checksum512URL, nil)
		if err != nil {
			return fmt.Errorf("download checksum: %w", err)
		}
		dbg("sha512 checksum saved to %s", checksum512Path)
	}

	if spec.Verify.Minisign.PublicKey != "" && spec.Verify.Minisign.SignedAsset != "" {
		sigAssetName, err2 := template.Resolve(spec.Verify.Minisign.SignedAsset, vars)
		if err2 != nil {
			return fmt.Errorf("resolve signed_asset: %w", err2)
		}
		sigURL, err2 := resolveAuxURL(sigAssetName)
		if err2 != nil {
			return fmt.Errorf("resolve signed_asset URL: %w", err2)
		}
		step("Downloading signature file...")
		dbg("minisign signature URL: %s", sigURL)
		sigPath, err = download.ToTemp(ctx, client, sigURL, nil)
		if err != nil {
			return fmt.Errorf("download signature: %w", err)
		}
		dbg("signature saved to %s", sigPath)
	}

	if !spec.Verify.Enabled() {
		dbg("no verification configured for this spec")
	}

	// 8. Verify the checksum's minisign signature (BEFORE downloading the artifact).
	if spec.Verify.Minisign.PublicKey != "" && sigPath != "" && checksumPath != "" {
		step("Verifying signature...")
		if err := verify.CheckMinisign(checksumPath, sigPath, spec.Verify.Minisign.PublicKey); err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}
		ok("Signature OK")
	}

	// 9. Download the artifact.
	step(fmt.Sprintf("Downloading %s...", assetName))
	artifactPath, err := download.ToTemp(ctx, client, downloadURL, progress)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer os.Remove(artifactPath)
	dbg("artifact saved to %s", artifactPath)
	ok(fmt.Sprintf("Downloaded %s", assetName))

	// 10. Verify SHA256/SHA512 integrity.
	verifyPlan := verify.Plan{
		ArtifactPath:    artifactPath,
		ArtifactName:    assetName,
		SHA256Literal:   spec.Verify.SHA256,
		SHA256AssetPath: checksumPath,
		SHA512Literal:   spec.Verify.SHA512,
		SHA512AssetPath: checksum512Path,
		MinisignPubKey:  spec.Verify.Minisign.PublicKey,
		MinisignSigPath: sigPath,
	}

	// Checksum verification only (minisign already verified above).
	if verifyPlan.SHA256Literal != "" || verifyPlan.SHA256AssetPath != "" ||
		verifyPlan.SHA512Literal != "" || verifyPlan.SHA512AssetPath != "" {
		step("Verifying integrity...")
		verifyPlan.MinisignPubKey = "" // already verified
		verifyPlan.MinisignSigPath = ""
		if err := verify.Run(verifyPlan); err != nil {
			return err
		}
		ok("Integrity OK")
	}

	// 11. Compute the artifact's SHA256 for the state.
	artifactSHA256, _ := filesha256(artifactPath)
	dbg("artifact sha256: %s", artifactSHA256)

	// 12. Install.
	step(fmt.Sprintf("Installing to %s...", dest))
	dbg("install: archive=%q strip_components=%d subdir=%q extract=%q chmod=%q", spec.Archive, spec.StripComponents, spec.Subdir, spec.Extract, spec.Chmod)
	archiveOpts := archive.ExtractOpts{
		StripComponents: spec.StripComponents,
		Subdir:          spec.Subdir,
	}

	chmod := spec.Chmod
	if app.Chmod != "" {
		chmod = app.Chmod
	}

	var kind string
	var installedFiles []string
	switch {
	case spec.Extract != "":
		// Dest is a file.
		kind = "file"
		extractName, err2 := template.Resolve(spec.Extract, vars)
		if err2 != nil {
			return fmt.Errorf("resolve extract: %w", err2)
		}
		if err := InstallFile(artifactPath, spec.Archive, extractName, dest, chmod); err != nil {
			return fmt.Errorf("install file: %w", err)
		}
	case len(spec.Binaries) > 0:
		// Dest is a directory (bin dir); each binary becomes a file inside it.
		// Works both with an archive (extraction by basename) and without one
		// (the downloaded artifact is the executable, e.g. binaries with os/arch/version in the name).
		kind = "binaries"
		bins := make([]ResolvedBinary, 0, len(spec.Binaries))
		for _, b := range spec.Binaries {
			var from, to string
			if b.From != "" {
				from, err = template.Resolve(b.From, vars)
				if err != nil {
					return fmt.Errorf("resolve binary from %q: %w", b.From, err)
				}
			}
			if b.To != "" {
				to, err = template.Resolve(b.To, vars)
				if err != nil {
					return fmt.Errorf("resolve binary to %q: %w", b.To, err)
				}
			}
			// Default for the installed name: From's basename, or (bare
			// download with no From) the name of the downloaded asset.
			if to == "" {
				if from != "" {
					to = filepath.Base(from)
				} else {
					to = assetName
				}
			}
			bins = append(bins, ResolvedBinary{From: from, To: to})
		}
		installedFiles, err = InstallBinaries(artifactPath, spec.Archive, bins, dest, chmod, archiveOpts)
		if err != nil {
			return fmt.Errorf("install binaries: %w", err)
		}
	default:
		// Dest is a directory.
		kind = "dir"
		if err := InstallDir(artifactPath, spec.Archive, dest, archiveOpts); err != nil {
			return fmt.Errorf("install dir: %w", err)
		}
	}
	ok(fmt.Sprintf("Installed %s %s → %s", appName, ver, dest))

	// 13. Record in the state DB (under a mutex to avoid races with other parallel goroutines).
	if err := state.Update(func(st *state.State) error {
		st.Set(state.InstalledApp{
			Name:        appName,
			Version:     ver,
			Kind:        kind,
			Dest:        dest,
			Files:       installedFiles,
			Source:      downloadURL,
			SHA256:      artifactSHA256,
			InstalledAt: time.Now().UTC(),
		})
		return nil
	}); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	dbg("state record saved: name=%q version=%q kind=%q", appName, ver, kind)

	return nil
}

// loadTemplates extracts the global and per-OS meta-templates from the config
// and the spec. Global templates (from templates.toml) have lower priority;
// the spec's templates override them.
func loadTemplates(cfg *config.Config, spec config.Spec) (template.MetaTemplates, map[string]template.MetaTemplates) {
	global := make(template.MetaTemplates)
	osOverrides := make(map[string]template.MetaTemplates)

	// 1. Global templates from templates.toml.
	for k, v := range cfg.GlobalTemplates {
		global[k] = v
	}
	for os, mt := range cfg.GlobalTemplatesOS {
		if osOverrides[os] == nil {
			osOverrides[os] = make(template.MetaTemplates)
		}
		for k, v := range mt {
			osOverrides[os][k] = v
		}
	}

	// 2. Templates from the spec (override the globals).
	for k, v := range spec.Templates {
		global[k] = v
	}
	for os, mt := range spec.TemplatesOS {
		if osOverrides[os] == nil {
			osOverrides[os] = make(template.MetaTemplates)
		}
		for k, v := range mt {
			osOverrides[os][k] = v
		}
	}

	return global, osOverrides
}

// buildAuxURL builds the URL of an auxiliary asset (checksum, signature) by
// substituting the main asset's name with the auxiliary one in the URL.
func buildAuxURL(downloadURL, assetName, auxName string) (string, error) {
	if !strings.HasSuffix(downloadURL, assetName) {
		return "", fmt.Errorf("cannot derive %q: download URL %q does not end with asset name %q", auxName, downloadURL, assetName)
	}
	return strings.TrimSuffix(downloadURL, assetName) + auxName, nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func filesha256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), nil
}
