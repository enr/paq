package archive

import (
	"fmt"
	"os"

	"github.com/klauspost/compress/zstd"
)

func extractTarZst(archivePath string, root *os.Root, opts ExtractOpts) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("zstd reader: %w", err)
	}
	defer zr.Close()

	return extractTar(zr, root, opts)
}
