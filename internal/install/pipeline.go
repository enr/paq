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

// Hooks permette alla pipeline di emettere eventi UI senza importare il package ui.
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

// shownError marca un errore già mostrato all'utente tramite l'hook OnFail,
// così i chiamanti non lo ristampano una seconda volta.
type shownError struct{ err error }

func (e shownError) Error() string { return e.err.Error() }
func (e shownError) Unwrap() error { return e.err }

// ErrAlreadyShown indica se err (o un errore che avvolge) è già stato mostrato
// all'utente dalla pipeline.
func ErrAlreadyShown(err error) bool {
	var se shownError
	return errors.As(err, &se)
}

// Run esegue la pipeline completa per installare una singola app.
// In caso di errore l'esito viene mostrato una sola volta tramite l'hook
// OnFail e l'errore ritornato viene marcato come già mostrato (shownError).
func Run(ctx context.Context, cfg *config.Config, appName string, progress download.ProgressFn, hooks *Hooks) (retErr error) {
	if hooks == nil {
		hooks = &Hooks{}
	}
	// Punto unico di presentazione dell'errore: qualunque return con errore
	// viene mostrato qui in rosso e marcato come "già mostrato".
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

	// 1. Trova app e ricetta nella config
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

	if spec.Extract != "" && len(spec.Binaries) > 0 {
		return fmt.Errorf("spec %q sets both 'extract' and 'binaries': they are mutually exclusive", specName)
	}

	// Rifiuta subito le piattaforme non supportate, prima di qualsiasi accesso
	// di rete. Il check usa i valori canonici grezzi (plat.OS/plat.Arch), non
	// quelli post-mappa per il tool.
	plat := platform.Detect()
	dbg("platform: os=%q arch=%q vendor=%q env=%q ext=%q", plat.OS, plat.Arch, plat.Vendor, plat.Env, plat.Ext)
	if !spec.SupportsPlatform(plat.OS, plat.Arch) {
		return fmt.Errorf("%q is not available for %s/%s (supported: %s)", specName, plat.OS, plat.Arch, strings.Join(spec.Platforms, ", "))
	}

	// Avvisa se la spec non configura alcuna verifica: il download non potrà
	// essere validato (integrità/firma). La verifica è la feature di sicurezza
	// principale, quindi l'assenza deve restare visibile.
	if !spec.Verify.Enabled() {
		warn(fmt.Sprintf("%q has no verification configured: integrity and signature cannot be checked", specName))
	}

	// 2. Risolvi la versione
	//   - versione omessa → default_version della spec; se assente, "latest"
	//   - "latest"        → risoluzione live via strategia/backend (no fallback)
	//   - "x.y.z"         → pin esplicito
	step(fmt.Sprintf("Resolving version for %s...", appName))
	var versionProvider version.Provider
	switch {
	case app.Version == "" && spec.DefaultVersion != "":
		versionProvider = version.PinProvider{Version: spec.DefaultVersion}
	case app.Version == "" || strings.EqualFold(app.Version, "latest"):
		versionProvider = version.LatestProvider(version.LatestRequest{
			Strategy: spec.LatestStrategy,
			Backend:  spec.Backend,
			Repo:     spec.Repo,
			Source:   spec.Source,
			ArchPkg:  spec.ArchPkg,
		})
	default:
		versionProvider = version.PinProvider{Version: app.Version}
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

	// 3. Costruisci le variabili di template
	versionMajor, versionMinor, versionPatch := version.Parse(ver)
	versionBuild := version.Build(tag)

	// Applica override per-OS dalla spec (es. jdk ha [jdk.darwin])
	spec = spec.ApplyOSOverride(plat.OS)

	// Applica override arch/os dalla spec (mappe [ripgrep.arch])
	resolvedArch := platform.ApplyMap(spec.Arch, plat.Arch, plat.Arch)
	resolvedOS := platform.ApplyMap(spec.OS, plat.OS, plat.OS)

	// Applica override da app manifest (ha la precedenza sulla spec)
	if app.Arch != nil {
		resolvedArch = platform.ApplyMap(app.Arch, plat.Arch, resolvedArch)
	}
	if app.OS != nil {
		resolvedOS = platform.ApplyMap(app.OS, plat.OS, resolvedOS)
	}

	vars := template.Vars{
		OS:           resolvedOS,
		Arch:         resolvedArch,
		Vendor:       plat.Vendor,
		Env:          plat.Env,
		Ext:          plat.Ext,
		Version:      ver,
		VersionMajor: versionMajor,
		VersionMinor: versionMinor,
		VersionPatch: versionPatch,
		VersionBuild: versionBuild,
	}

	// 4. Espandi i meta-template
	// Carica templates.toml embedded
	globalMT, osMT := loadTemplates(cfg, spec)
	vars, err = template.Expand(globalMT, osMT, vars)
	if err != nil {
		return fmt.Errorf("expand meta-templates: %w", err)
	}

	// 5. Risolvi dest. Se l'app non lo specifica, deriva il default dalla spec
	// e dalle directory base configurate dall'utente ([defaults]).
	destTemplate := app.Dest
	if destTemplate == "" {
		destTemplate = config.DefaultDest(spec, appName, cfg.Defaults)
	}
	dest, err := template.Resolve(destTemplate, vars)
	if err != nil {
		return fmt.Errorf("resolve dest: %w", err)
	}
	dest = expandHome(dest)

	// 6. Risolvi URL artefatto
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
	dbg("resolved: os=%q arch=%q dest=%q", resolvedOS, resolvedArch, dest)

	// Variabili per i nomi degli asset
	assetName := filepath.Base(downloadURL)
	if vars.Extra == nil {
		vars.Extra = make(map[string]string)
	}
	// {{asset}} di default è il basename dell'URL: utile per il backend "url"
	// (es. maven) dove non esiste un campo asset esplicito.
	vars.Extra["asset"] = assetName
	// Risolvi il nome dell'asset dal template e aggiungilo a vars.Extra
	// così che {{asset}} sia disponibile nei template successivi (es. sha256_asset)
	if spec.Asset != "" {
		if name, err2 := template.Resolve(spec.Asset, vars); err2 == nil {
			assetName = name
			vars.Extra["asset"] = name
		}
	}
	dbg("asset name: %q", assetName)

	// resolveAuxURL costruisce l'URL di un asset ausiliario (checksum, firma) in
	// modo backend-aware. Per il backend "github" l'URL di download è l'endpoint
	// API dell'asset (…/releases/assets/{id}), che non contiene il nome file:
	// l'asset ausiliario va quindi risolto per nome via l'API. Per gli altri
	// backend l'URL contiene il nome file e basta sostituirlo.
	resolveAuxURL := func(auxName string) (string, error) {
		if spec.Backend == "github" {
			gb := backend.GitHubBackend{Repo: spec.Repo, Asset: auxName}
			return gb.Resolve(ctx, tag, vars)
		}
		return buildAuxURL(downloadURL, assetName, auxName), nil
	}

	client := download.NewClient()

	// 7. Scarica file checksum/firma (se configurati)
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
		step(fmt.Sprintf("Downloading checksum file..."))
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

	// 8. Verifica firma minisign del checksum (PRIMA di scaricare l'artefatto)
	if spec.Verify.Minisign.PublicKey != "" && sigPath != "" && checksumPath != "" {
		step("Verifying signature...")
		if err := verify.CheckMinisign(checksumPath, sigPath, spec.Verify.Minisign.PublicKey); err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}
		ok("Signature OK")
	}

	// 9. Scarica artefatto
	step(fmt.Sprintf("Downloading %s...", assetName))
	artifactPath, err := download.ToTemp(ctx, client, downloadURL, progress)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer os.Remove(artifactPath)
	dbg("artifact saved to %s", artifactPath)
	ok(fmt.Sprintf("Downloaded %s", assetName))

	// 10. Verifica integrità SHA256/SHA512
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

	// Solo verifica checksum (minisign già verificata sopra)
	if verifyPlan.SHA256Literal != "" || verifyPlan.SHA256AssetPath != "" ||
		verifyPlan.SHA512Literal != "" || verifyPlan.SHA512AssetPath != "" {
		step("Verifying integrity...")
		verifyPlan.MinisignPubKey = "" // già verificata
		verifyPlan.MinisignSigPath = ""
		if err := verify.Run(verifyPlan); err != nil {
			return err
		}
		ok("Integrity OK")
	}

	// 11. Calcola SHA256 dell'artefatto per lo state
	artifactSHA256, _ := filesha256(artifactPath)
	dbg("artifact sha256: %s", artifactSHA256)

	// 12. Installa
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
		// Dest è un file
		kind = "file"
		extractName, err2 := template.Resolve(spec.Extract, vars)
		if err2 != nil {
			return fmt.Errorf("resolve extract: %w", err2)
		}
		if err := InstallFile(artifactPath, spec.Archive, extractName, dest, chmod); err != nil {
			return fmt.Errorf("install file: %w", err)
		}
	case len(spec.Binaries) > 0:
		// Dest è una directory (bin dir); ogni binario diventa un file al suo interno.
		// Funziona sia con archivio (estrazione per basename) sia senza archivio
		// (l'artefatto scaricato è l'eseguibile, es. binari con os/arch/versione nel nome).
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
			// Default per il nome installato: basename di From, oppure (download
			// nudo senza From) il nome dell'asset scaricato.
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
		// Dest è una directory
		kind = "dir"
		if err := InstallDir(artifactPath, spec.Archive, dest, archiveOpts); err != nil {
			return fmt.Errorf("install dir: %w", err)
		}
	}
	ok(fmt.Sprintf("Installed %s %s → %s", appName, ver, dest))

	// 13. Registra nello state DB (sotto mutex per evitare race con altre goroutine parallele)
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

// loadTemplates estrae i meta-template globali e per-OS dalla config e dalla spec.
// I template globali (da templates.toml) hanno priorità più bassa; quelli della spec li sovrascrivono.
func loadTemplates(cfg *config.Config, spec config.Spec) (template.MetaTemplates, map[string]template.MetaTemplates) {
	global := make(template.MetaTemplates)
	osOverrides := make(map[string]template.MetaTemplates)

	// 1. Template globali da templates.toml
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

	// 2. Template dalla spec (sovrascrivono i globali)
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

// buildAuxURL costruisce l'URL di un asset ausiliario (checksum, firma)
// sostituendo il nome dell'asset principale con quello ausiliario nell'URL.
func buildAuxURL(downloadURL, assetName, auxName string) string {
	base := strings.TrimSuffix(downloadURL, assetName)
	return base + auxName
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
