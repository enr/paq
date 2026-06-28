package archive

import (
	"archive/zip"
	"fmt"
	"path/filepath"
	"strings"
)

func extractZip(archivePath string, opts ExtractOpts) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", archivePath, err)
	}
	defer zr.Close()

	found := false

	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		name = strings.TrimSuffix(name, "/")

		stripped, ok := stripComponents(name, opts.StripComponents)
		if !ok || stripped == "" {
			continue
		}

		switch {
		case opts.Extract != "":
			if filepath.Base(stripped) == opts.Extract {
				dest := filepath.Join(opts.Dest, opts.Extract)
				rc, err := f.Open()
				if err != nil {
					return err
				}
				werr := writeFile(rc, dest, f.Mode())
				rc.Close()
				if werr != nil {
					return werr
				}
				found = true
			}

		case opts.Subdir != "":
			rel, match := matchSubdir(stripped, opts.Subdir)
			if !match || rel == "" {
				continue
			}
			dest := filepath.Join(opts.Dest, filepath.FromSlash(rel))
			if f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			werr := writeFile(rc, dest, f.Mode())
			rc.Close()
			if werr != nil {
				return werr
			}

		default:
			dest := filepath.Join(opts.Dest, filepath.FromSlash(stripped))
			if f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			werr := writeFile(rc, dest, f.Mode())
			rc.Close()
			if werr != nil {
				return werr
			}
		}
	}

	if opts.Extract != "" && !found {
		return fmt.Errorf("file %q not found in zip", opts.Extract)
	}
	return nil
}
