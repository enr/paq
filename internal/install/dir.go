package install

import (
	"fmt"
	"os"

	"github.com/enr/paq/internal/archive"
)

// InstallDir extracts the archive's tree into dest with a safe swap.
// If dest already exists: dest → dest.bak, dest.tmp → dest, remove dest.bak.
// If any step after extraction fails, dest is left intact.
func InstallDir(archivePath, archiveType, dest string, opts archive.ExtractOpts) error {
	destTmp := dest + ".tmp"
	destBak := dest + ".bak"

	// Clean up any leftovers from previous failed runs.
	os.RemoveAll(destTmp)
	os.RemoveAll(destBak)

	opts.Dest = destTmp
	if err := archive.Extract(archivePath, archiveType, opts); err != nil {
		os.RemoveAll(destTmp)
		return fmt.Errorf("extract: %w", err)
	}

	// Swap: dest → dest.bak, dest.tmp → dest.
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, destBak); err != nil {
			os.RemoveAll(destTmp)
			return fmt.Errorf("backup dest: %w", err)
		}
	}

	if err := os.Rename(destTmp, dest); err != nil {
		// Try to roll back the backup.
		os.Rename(destBak, dest)
		return fmt.Errorf("swap dir: %w", err)
	}

	// Remove the backup.
	os.RemoveAll(destBak)
	return nil
}
