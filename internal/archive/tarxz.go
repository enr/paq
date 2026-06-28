package archive

import (
	"fmt"
	"os"

	"github.com/ulikunitz/xz"
)

func extractTarXz(archivePath string, opts ExtractOpts) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer f.Close()

	xzr, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("xz reader: %w", err)
	}

	return extractTar(xzr, opts)
}
