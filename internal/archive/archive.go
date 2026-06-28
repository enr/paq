package archive

import (
	"fmt"
	"os"
)

// ExtractOpts configura come estrarre un archivio.
type ExtractOpts struct {
	// StripComponents rimuove i primi N componenti del path di ogni entry.
	StripComponents int
	// Extract: se non vuoto, estrae solo il file con questo nome (solo basename).
	Extract string
	// Subdir: se non vuoto, estrae solo i file il cui path (dopo StripComponents) ha questo prefisso.
	// Supporta glob per il primo componente (es. "*/Contents/Home").
	Subdir string
	// Dest è la directory di destinazione.
	Dest string
}

// Extract sceglie il metodo di estrazione in base ad archiveType e lo esegue.
func Extract(archivePath string, archiveType string, opts ExtractOpts) error {
	if err := os.MkdirAll(opts.Dest, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	switch archiveType {
	case "tar.gz", "tgz":
		return extractTarGz(archivePath, opts)
	case "tar.xz":
		return extractTarXz(archivePath, opts)
	case "zip":
		return extractZip(archivePath, opts)
	default:
		return fmt.Errorf("unsupported archive type: %q", archiveType)
	}
}
