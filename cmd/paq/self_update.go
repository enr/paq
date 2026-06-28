package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/enr/paq/internal/archive"
	"github.com/enr/paq/internal/backend"
	"github.com/enr/paq/internal/download"
	"github.com/enr/paq/internal/platform"
	"github.com/enr/paq/internal/template"
	"github.com/enr/paq/internal/ui"
	"github.com/enr/paq/internal/verify"
	"github.com/enr/paq/internal/version"
	"github.com/spf13/cobra"
)

// selfUpdateRepo è la repo GitHub da cui scaricare i binari di paq.
const selfUpdateRepo = "enr/paq"

// selfUpdateChecksums è il nome dell'asset col checksum sha256 nelle release.
const selfUpdateChecksums = "SHA256SUMS"

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

// selfUpdateAssetName costruisce il nome dell'asset zip della release per tag/piattaforma.
// Esempio: ("v0.1.0", "linux", "amd64") → "paq-v0.1.0-linux-amd64.zip".
func selfUpdateAssetName(tag, os, arch string) string {
	return fmt.Sprintf("paq-%s-%s-%s.zip", tag, os, arch)
}

func runSelfUpdate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	check, _ := cmd.Flags().GetBool("check")
	force, _ := cmd.Flags().GetBool("force")

	ui.Step("Resolving latest paq release...")
	latest, tag, err := version.GitHubReleaseProvider{Repo: selfUpdateRepo}.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("resolve latest release: %w", err)
	}

	current := version.Clean(Version)
	upToDate := current == latest

	if upToDate && !force {
		ui.OK("paq is already up to date (%s)", latest)
		return nil
	}
	if check {
		ui.Info("Update available: paq %s → %s", Version, latest)
		return nil
	}
	// Build non versionato (es. "dev"): non possiamo confrontare le versioni,
	// procediamo comunque ma avvisiamo l'utente.
	if current == Version && Version != latest {
		ui.Warn("current version %q is not a release version, updating to %s", Version, latest)
	}
	ui.Info("Updating paq %s → %s", Version, latest)

	// Risolvi gli URL degli asset per la piattaforma corrente.
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
	gb := backend.GitHubBackend{Repo: selfUpdateRepo, Asset: assetName}
	url, err := gb.Resolve(ctx, tag, vars)
	if err != nil {
		return fmt.Errorf("resolve release asset: %w", err)
	}

	sumsURL, err := backend.GitHubBackend{Repo: selfUpdateRepo, Asset: selfUpdateChecksums}.Resolve(ctx, tag, vars)
	if err != nil {
		return fmt.Errorf("resolve checksums asset: %w", err)
	}

	ui.Step("Downloading %s...", assetName)
	zipPath, err := download.ToTemp(ctx, &http.Client{}, url, ui.NewProgressFn("paq"))
	if err != nil {
		return fmt.Errorf("download release: %w", err)
	}
	defer os.Remove(zipPath)

	sumsPath, err := download.ToTemp(ctx, &http.Client{}, sumsURL, nil)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	defer os.Remove(sumsPath)

	ui.Step("Verifying checksum...")
	if err := verify.Run(verify.Plan{
		SHA256AssetPath: sumsPath,
		ArtifactName:    assetName,
		ArtifactPath:    zipPath,
	}); err != nil {
		return err
	}

	// Estrai il binario dall'archivio zip.
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

// replaceExecutable sostituisce il binario in exePath con quello in newBinary.
// Scrive prima in un file temporaneo nella stessa directory così che la rename
// finale sia atomica e sullo stesso filesystem.
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
	defer os.Remove(tmpName) // no-op se la rename ha successo

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
		// Su alcune piattaforme non si può sovrascrivere un binario in esecuzione:
		// sposta prima il vecchio da parte, poi metti il nuovo al suo posto.
		backup := exePath + ".old"
		if rerr := os.Rename(exePath, backup); rerr != nil {
			return fmt.Errorf("replace executable: %w", err)
		}
		if rerr := os.Rename(tmpName, exePath); rerr != nil {
			os.Rename(backup, exePath) // ripristina il vecchio binario
			return fmt.Errorf("replace executable: %w", rerr)
		}
		os.Remove(backup)
	}
	return nil
}
