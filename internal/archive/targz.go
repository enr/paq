package archive

import (
	"compress/gzip"
	"fmt"
	"os"
)

func extractTarGz(archivePath string, opts ExtractOpts) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	return extractTar(gz, opts)
}
