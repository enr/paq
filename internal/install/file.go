package install

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/enr/paq/internal/archive"
)

// InstallFile extracts the single binary from the archive and atomically installs it to dest.
// If extractName is empty, the archive is extracted directly as a single file.
func InstallFile(archivePath, archiveType, extractName, dest, chmod string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	// Extract into a temp directory in the same dir as dest
	// (same filesystem → atomic cross-device rename guaranteed).
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

	// Apply chmod.
	mode, err := parseFileMode(chmod)
	if err != nil {
		return err
	}
	if mode != 0 {
		if err := os.Chmod(extracted, mode); err != nil {
			return fmt.Errorf("chmod: %w", err)
		}
	}

	// Atomic rename onto dest.
	if err := os.Rename(extracted, dest); err != nil {
		return fmt.Errorf("install file: %w", err)
	}

	return nil
}
