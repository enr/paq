package archive

import (
	"archive/zip"
	"fmt"
	"os"
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

		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("entry %q is a symlink: not supported", f.Name)
		}

		switch {
		case opts.Extract != "":
			if filepath.Base(stripped) == opts.Extract {
				dest, err := securePath(opts.Dest, opts.Extract)
				if err != nil {
					return err
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
				found = true
			}

		case opts.Subdir != "":
			rel, match := matchSubdir(stripped, opts.Subdir)
			if !match || rel == "" {
				continue
			}
			dest, err := securePath(opts.Dest, rel)
			if err != nil {
				return err
			}
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
			dest, err := securePath(opts.Dest, stripped)
			if err != nil {
				return err
			}
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
