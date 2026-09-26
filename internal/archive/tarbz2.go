package archive

import (
	"compress/bzip2"
	"fmt"
	"os"
)

func extractTarBz2(archivePath string, root *os.Root, opts ExtractOpts) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer f.Close()

	return extractTar(bzip2.NewReader(f), root, opts)
}
