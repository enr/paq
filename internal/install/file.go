package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/enr/paq/internal/archive"
)

// InstallFile estrae il singolo binario dall'archivio e lo installa atomicamente in dest.
// Se extractName è vuoto, l'archivio viene estratto direttamente come file singolo.
func InstallFile(archivePath, archiveType, extractName, dest, chmod string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	// Estrai in una directory temporanea nella stessa dir di dest
	// (stesso filesystem → rename atomico cross-device garantito)
	tmpDir, err := os.MkdirTemp(filepath.Dir(dest), "paq-install-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := archive.ExtractOpts{
		Extract: extractName,
		Dest:    tmpDir,
	}

	if err := archive.Extract(archivePath, archiveType, opts); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	extracted := filepath.Join(tmpDir, extractName)

	// Applica chmod
	if chmod != "" {
		mode, err := strconv.ParseUint(chmod, 8, 32)
		if err != nil {
			return fmt.Errorf("parse chmod %q: %w", chmod, err)
		}
		if err := os.Chmod(extracted, os.FileMode(mode)); err != nil {
			return fmt.Errorf("chmod: %w", err)
		}
	}

	// Rename atomico su dest
	if err := os.Rename(extracted, dest); err != nil {
		return fmt.Errorf("install file: %w", err)
	}

	return nil
}
