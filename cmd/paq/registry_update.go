package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enr/paq/internal/archive"
	"github.com/enr/paq/internal/backend"
	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/download"
	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/template"
	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/verify"
	"github.com/enr/paq/internal/version"
	"github.com/spf13/cobra"
)

// Registry release asset names, published alongside each paq release.
const (
	registryAsset     = "registry.tar.gz"
	registryChecksums = "registry.tar.gz.sha256"
	registrySignature = "registry.tar.gz.sha256.minisig"
)

// registryMaxBytes caps the downloaded archive size. The real registry is a
// few KB; the cap bounds a malicious or broken source before extraction.
// A var (not const) so tests can lower it.
var registryMaxBytes int64 = 10 << 20 // 10 MiB

// registryUpdateClient builds the HTTP client used to fetch registry assets.
// Overridable in tests.
var registryUpdateClient = download.NewClient

var registryUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Download and install the latest registry snapshot",
	Long: "Download the registry published with the latest paq release " +
		"(or from the source configured in [registry]), verify its signature, " +
		"and install it as the external registry snapshot that overlays the " +
		"embedded definitions.",
	Args: cobra.NoArgs,
	RunE: runRegistryUpdate,
}

func init() {
	registryUpdateCmd.Flags().BoolP("force", "f", false, "Reinstall even if already up to date or older")
	registryCmd.AddCommand(registryUpdateCmd)
}

func runRegistryUpdate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	force, _ := cmd.Flags().GetBool("force")

	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return fmt.Errorf("load user config: %w", err)
	}

	src, err := resolveRegistrySource(ctx, userCfg.Registry)
	if err != nil {
		return err
	}

	client := registryUpdateClient()

	ui.Step("Downloading registry...")
	tarPath, err := download.ToTemp(ctx, client, src.tarURL, ui.NewProgressFn("registry"))
	if err != nil {
		return fmt.Errorf("download registry: %w", err)
	}
	defer os.Remove(tarPath)

	info, err := os.Stat(tarPath)
	if err != nil {
		return err
	}
	if info.Size() > registryMaxBytes {
		return fmt.Errorf("registry archive is too large (%d bytes, max %d)", info.Size(), registryMaxBytes)
	}

	sumsPath, err := download.ToTemp(ctx, client, src.sumsURL, nil)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	defer os.Remove(sumsPath)

	// Signature verification is temporarily optional: a build without an
	// embedded public key (src.pubKey == "") verifies the checksum only.
	// See plan/registry-signing-enablement.md for re-enabling enforcement.
	var sigPath string
	if src.pubKey != "" {
		sigPath, err = download.ToTemp(ctx, client, src.sigURL, nil)
		if err != nil {
			return fmt.Errorf("download signature: %w", err)
		}
		defer os.Remove(sigPath)
		ui.Step("Verifying signature...")
	} else {
		ui.Warn("this build has no registry signing key: signature not verified (checksum only)")
		ui.Step("Verifying checksum...")
	}
	if err := verify.Run(verify.Plan{
		SHA256AssetPath: sumsPath,
		ArtifactName:    registryAsset,
		ArtifactPath:    tarPath,
		MinisignPubKey:  src.pubKey,
		MinisignSigPath: sigPath,
	}); err != nil {
		return err
	}

	// Extract only after the signature has been verified.
	staging, err := registry.StagingDir()
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(staging) // no-op once Install renames it into place

	if err := archive.Extract(tarPath, "tar.gz", archive.ExtractOpts{Dest: staging}); err != nil {
		return fmt.Errorf("extract registry: %w", err)
	}

	// Validate the snapshot before swapping it in, so the cache can never end
	// up in a state loadConfig would have to degrade around.
	stagingFS := os.DirFS(staging)
	specs, err := config.LoadEmbeddedRegistry(stagingFS)
	if err != nil {
		return fmt.Errorf("invalid registry archive: %w", err)
	}
	if len(specs) == 0 {
		return fmt.Errorf("registry archive contains no recipes")
	}
	if _, _, err := config.LoadGlobalTemplates(stagingFS); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("invalid registry templates: %w", err)
	}
	newVersion, err := readRegistryVersion(staging)
	if err != nil {
		return err
	}

	// Best-effort downgrade / no-op protection against the cached snapshot.
	if _, cur, _ := registry.Open(); cur != nil && !force {
		cmp := version.Compare(version.Clean(newVersion), version.Clean(cur.Version))
		if cmp == 0 {
			ui.OK("registry already up to date (%s)", newVersion)
			return nil
		}
		if cmp < 0 {
			return fmt.Errorf("refusing to downgrade registry from %s to %s (use --force)", cur.Version, newVersion)
		}
	}

	meta := registry.Meta{
		Tag:       src.tag,
		Version:   newVersion,
		FetchedAt: time.Now().UTC(),
		SourceURL: src.tarURL,
		SpecCount: len(specs),
	}
	if err := registry.Install(staging, meta); err != nil {
		return fmt.Errorf("install registry snapshot: %w", err)
	}

	ui.OK("registry updated to %s (%d recipes)", newVersion, len(specs))
	return nil
}

// registrySource holds the resolved URLs and trust anchor for an update.
type registrySource struct {
	tarURL  string
	sumsURL string
	sigURL  string
	pubKey  string
	tag     string
}

// resolveRegistrySource determines where to fetch the registry from: a custom
// [registry].url (which must be https and carry its own public_key) or the
// default paq release assets. For the default source, an empty embedded
// signing key downgrades the update to checksum-only verification (temporary,
// until the signing infrastructure is enabled: see
// plan/registry-signing-enablement.md).
func resolveRegistrySource(ctx context.Context, rs config.RegistrySettings) (registrySource, error) {
	if rs.URL != "" {
		if !strings.HasPrefix(rs.URL, "https://") {
			return registrySource{}, fmt.Errorf("custom registry url must use https://")
		}
		if rs.PublicKey == "" {
			return registrySource{}, fmt.Errorf("custom registry url requires public_key in [registry]")
		}
		return registrySource{
			tarURL:  rs.URL,
			sumsURL: rs.URL + ".sha256",
			sigURL:  rs.URL + ".sha256.minisig",
			pubKey:  rs.PublicKey,
			tag:     "custom",
		}, nil
	}

	ui.Step("Resolving latest registry release...")
	_, tag, err := version.GitHubReleaseProvider{Repo: selfUpdateRepo}.Resolve(ctx)
	if err != nil {
		return registrySource{}, fmt.Errorf("resolve latest release: %w", err)
	}
	vars := template.Vars{Version: version.Clean(tag)}

	resolve := func(asset string) (string, error) {
		return backend.GitHubBackend{Repo: selfUpdateRepo, Asset: asset}.Resolve(ctx, tag, vars)
	}
	tarURL, err := resolve(registryAsset)
	if err != nil {
		return registrySource{}, fmt.Errorf("resolve registry asset: %w", err)
	}
	sumsURL, err := resolve(registryChecksums)
	if err != nil {
		return registrySource{}, fmt.Errorf("resolve checksums asset: %w", err)
	}
	sigURL, err := resolve(registrySignature)
	if err != nil {
		return registrySource{}, fmt.Errorf("resolve signature asset: %w", err)
	}
	return registrySource{
		tarURL:  tarURL,
		sumsURL: sumsURL,
		sigURL:  sigURL,
		pubKey:  registry.DefaultPublicKey,
		tag:     tag,
	}, nil
}

// readRegistryVersion reads and validates the VERSION file shipped inside the
// registry archive (written by the release pipeline).
func readRegistryVersion(staging string) (string, error) {
	data, err := os.ReadFile(filepath.Join(staging, "registry", "VERSION"))
	if err != nil {
		return "", fmt.Errorf("registry archive has no VERSION file: %w", err)
	}
	v := strings.TrimSpace(string(data))
	if v == "" {
		return "", fmt.Errorf("registry archive has an empty VERSION file")
	}
	return v, nil
}
