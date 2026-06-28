package install

import (
	"fmt"
	"os"

	"github.com/enr/paq/internal/archive"
)

// InstallDir estrae l'albero dell'archivio in dest con swap sicuro.
// Se dest esiste già: dest → dest.bak, dest.tmp → dest, rimuovi dest.bak.
// Se qualsiasi passo dopo l'estrazione fallisce, dest resta intatto.
func InstallDir(archivePath, archiveType, dest string, opts archive.ExtractOpts) error {
	destTmp := dest + ".tmp"
	destBak := dest + ".bak"

	// Pulisci eventuali residui di run precedenti fallite
	os.RemoveAll(destTmp)
	os.RemoveAll(destBak)

	opts.Dest = destTmp
	if err := archive.Extract(archivePath, archiveType, opts); err != nil {
		os.RemoveAll(destTmp)
		return fmt.Errorf("extract: %w", err)
	}

	// Swap: dest → dest.bak, dest.tmp → dest
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, destBak); err != nil {
			os.RemoveAll(destTmp)
			return fmt.Errorf("backup dest: %w", err)
		}
	}

	if err := os.Rename(destTmp, dest); err != nil {
		// Prova rollback del backup
		os.Rename(destBak, dest)
		return fmt.Errorf("swap dir: %w", err)
	}

	// Rimuovi il backup
	os.RemoveAll(destBak)
	return nil
}
