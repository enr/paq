package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/enr/paq/internal/archive"
	"github.com/enr/paq/internal/backend"
	"github.com/enr/paq/internal/download"
	"github.com/enr/paq/internal/platform"
	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/template"
	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/verify"
	"github.com/enr/paq/internal/version"
	"github.com/spf13/cobra"
)

// selfUpdateRepo is the GitHub repo to download paq binaries from.
const selfUpdateRepo = "enr/paq"

// selfUpdateChecksums is the name of the sha256 checksum asset in releases.
const selfUpdateChecksums = "SHA256SUMS"

// selfUpdateChecksumsSig is the name of SHA256SUMS' minisign signature asset,
// published alongside it starting with this release. Signed with the same
// key pair as the registry (one trust anchor, see registry.DefaultPublicKey).
const selfUpdateChecksumsSig = "SHA256SUMS.minisig"

// selfUpdateClient builds the HTTP client used for the self-update's GitHub
// requests and downloads. Overridable in tests (mirrors registryUpdateClient).
var selfUpdateClient = download.NewClient

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update paq itself to the latest release",
	Long:  "Download the latest release of paq from GitHub and replace the running binary.",
	Args:  cobra.NoArgs,
	RunE:  runSelfUpdate,
}

func init() {
	selfUpdateCmd.Flags().BoolP("check", "c", false, "Only check for an available update, don't install")
	selfUpdateCmd.Flags().BoolP("force", "f", false, "Reinstall even if already up to date")
	rootCmd.AddCommand(selfUpdateCmd)
}

// selfUpdateAssetName builds the release zip asset name for a given tag/platform.
// Example: ("v0.1.0", "linux", "amd64") → "paq-v0.1.0-linux-amd64.zip".
func selfUpdateAssetName(tag, os, arch string) string {
	return fmt.Sprintf("paq-%s-%s-%s.zip", tag, os, arch)
}

func runSelfUpdate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	check, _ := cmd.Flags().GetBool("check")
	force, _ := cmd.Flags().GetBool("force")

	client := selfUpdateClient()

	ui.Step("Resolving latest paq release...")
	latest, tag, err := (version.GitHubReleaseProvider{Repo: selfUpdateRepo, HTTPClient: client}).Resolve(ctx)
	if err != nil {
		return fmt.Errorf("resolve latest release: %w", err)
	}

	current := version.Clean(Version)
	cmp := version.Compare(current, latest)
	upToDate := cmp >= 0

	if upToDate && !force {
		if cmp > 0 {
			ui.OK("paq %s is ahead of the latest release (%s)", Version, latest)
		} else {
			ui.OK("paq is already up to date (%s)", latest)
		}
		return nil
	}
	if check {
		ui.Step("Update available: paq %s → %s", Version, latest)
		return nil
	}
	// Unversioned build (e.g. "dev"): we can't compare versions,
	// proceed anyway but warn the user.
	if current == Version && Version != latest {
		ui.Warn("current version %q is not a release version, updating to %s", Version, latest)
	}
	ui.Step("Updating paq %s → %s", Version, latest)

	// Resolve the asset URLs for the current platform.
	plat := platform.Detect()
	vars := template.Vars{
		OS:      plat.OS,
		Arch:    plat.Arch,
		Vendor:  plat.Vendor,
		Env:     plat.Env,
		Ext:     plat.Ext,
		Version: latest,
	}

	assetName := selfUpdateAssetName(tag, plat.OS, plat.Arch)
	ui.Debug("asset name: %s", assetName)

	zipPath, err := downloadAndVerifyRelease(ctx, client, tag, vars, assetName, ui.NewProgressFn("paq"))
	if err != nil {
		return err
	}
	defer os.Remove(zipPath)

	// Extract the binary from the zip archive.
	binName := "paq" + plat.Ext
	extractDir, err := os.MkdirTemp("", "paq-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(extractDir)

	if err := archive.Extract(zipPath, "zip", archive.ExtractOpts{Extract: binName, Dest: extractDir}); err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}
	newBinary := filepath.Join(extractDir, binName)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}

	if err := replaceExecutable(exePath, newBinary); err != nil {
		return err
	}

	ui.OK("paq updated to %s (%s)", latest, exePath)
	return nil
}

// downloadAndVerifyRelease resolves the release asset and checksums URLs,
// downloads them, and verifies the artifact's checksum before returning the
// path of the verified zip archive (the caller must remove it). When the
// running build has a registry public key embedded (registry.DefaultPublicKey,
// set via -ldflags at release build time), it also requires and verifies a
// minisign signature over SHA256SUMS: transport integrity alone is not
// authenticity. There is no fallback to unsigned — a release published
// before the signature asset existed fails outright when the build has a
// key, instead of silently downgrading to checksum-only.
func downloadAndVerifyRelease(ctx context.Context, client *http.Client, tag string, vars template.Vars, assetName string, progress download.ProgressFn) (zipPath string, err error) {
	gb := backend.GitHubBackend{Repo: selfUpdateRepo, Asset: assetName, HTTPClient: client}
	url, err := gb.Resolve(ctx, tag, vars)
	if err != nil {
		return "", fmt.Errorf("resolve release asset: %w", err)
	}
	ui.Debug("asset URL: %s", url)

	sumsURL, err := (backend.GitHubBackend{Repo: selfUpdateRepo, Asset: selfUpdateChecksums, HTTPClient: client}).Resolve(ctx, tag, vars)
	if err != nil {
		return "", fmt.Errorf("resolve checksums asset: %w", err)
	}
	ui.Debug("checksums URL: %s", sumsURL)

	ui.Step("Downloading %s...", assetName)
	zipPath, err = download.ToTemp(ctx, client, url, progress)
	if err != nil {
		return "", fmt.Errorf("download release: %w", err)
	}
	cleanup := func() { os.Remove(zipPath) }

	sumsPath, err := download.ToTemp(ctx, client, sumsURL, nil)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("download checksums: %w", err)
	}
	defer os.Remove(sumsPath)

	verifyPlan := verify.Plan{
		SHA256AssetPath: sumsPath,
		ArtifactName:    assetName,
		ArtifactPath:    zipPath,
	}

	if registry.DefaultPublicKey != "" {
		sigURL, err := (backend.GitHubBackend{Repo: selfUpdateRepo, Asset: selfUpdateChecksumsSig, HTTPClient: client}).Resolve(ctx, tag, vars)
		if err != nil {
			cleanup()
			return "", fmt.Errorf("resolve checksums signature asset: %w", err)
		}
		ui.Debug("checksums signature URL: %s", sigURL)

		sigPath, err := download.ToTemp(ctx, client, sigURL, nil)
		if err != nil {
			cleanup()
			return "", fmt.Errorf("download checksums signature: %w", err)
		}
		defer os.Remove(sigPath)

		verifyPlan.MinisignPubKey = registry.DefaultPublicKey
		verifyPlan.MinisignSigPath = sigPath
		ui.Step("Verifying signature...")
	} else {
		ui.Step("Verifying checksum...")
	}

	if err := verify.Run(verifyPlan); err != nil {
		cleanup()
		return "", err
	}
	return zipPath, nil
}

// replaceExecutable replaces the binary at exePath with the one at newBinary.
// It first writes to a temp file in the same directory so that the final
// rename is atomic and stays on the same filesystem.
func replaceExecutable(exePath, newBinary string) error {
	dir := filepath.Dir(exePath)

	src, err := os.Open(newBinary)
	if err != nil {
		return fmt.Errorf("open downloaded binary: %w", err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(dir, ".paq-update-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if the rename succeeds

	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return fmt.Errorf("write new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close new binary: %w", err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("chmod new binary: %w", err)
	}

	if err := os.Rename(tmpName, exePath); err != nil {
		// On some platforms a running binary cannot be overwritten:
		// move the old one aside first, then put the new one in its place.
		backup := exePath + ".old"
		if rerr := os.Rename(exePath, backup); rerr != nil {
			return fmt.Errorf("replace executable: %w", err)
		}
		if rerr := os.Rename(tmpName, exePath); rerr != nil {
			os.Rename(backup, exePath) // restore the old binary
			return fmt.Errorf("replace executable: %w", rerr)
		}
		os.Remove(backup)
	}
	return nil
}
