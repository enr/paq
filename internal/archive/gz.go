package archive

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

// Gunzip decompresses src, a gzip-compressed single file (archive = "gz"),
// into a new temp file created in dir, and returns its path. The caller
// removes it.
func Gunzip(src, dir string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	gz, err := gzip.NewReader(in)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	out, err := os.CreateTemp(dir, "paq-gunzip-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(out, gz); err != nil {
		out.Close()
		os.Remove(out.Name())
		return "", fmt.Errorf("decompress %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", fmt.Errorf("close %s: %w", out.Name(), err)
	}
	return out.Name(), nil
}
