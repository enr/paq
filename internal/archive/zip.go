package archive

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func extractZip(archivePath string, root *os.Root, opts ExtractOpts) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", archivePath, err)
	}
	defer zr.Close()

	wanted := extractSet(opts.Extracts)
	found := make(map[string]bool, len(wanted))

	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		name = strings.TrimSuffix(name, "/")

		stripped, ok := stripComponents(name, opts.StripComponents)
		if !ok || stripped == "" {
			continue
		}

		isSymlink := f.Mode()&os.ModeSymlink != 0

		switch {
		case wanted != nil:
			if f.FileInfo().IsDir() {
				continue
			}
			base := filepath.Base(stripped)
			if !wanted[base] {
				continue
			}
			if isSymlink {
				return symlinkExtractError(f.Name)
			}
			if found[base] {
				return fmt.Errorf("multiple files named %q in archive: ambiguous extract", base)
			}
			dest, err := securePath(base)
			if err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			werr := writeFile(root, dest, rc, f.Mode())
			rc.Close()
			if werr != nil {
				return werr
			}
			found[base] = true

		case opts.Subdir != "":
			rel, match := matchSubdir(stripped, opts.Subdir)
			if !match || rel == "" {
				continue
			}
			if isSymlink {
				return zipSymlinkError(f.Name)
			}
			dest, err := securePath(rel)
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
			werr := writeFile(root, dest, rc, f.Mode())
			rc.Close()
			if werr != nil {
				return werr
			}

		default:
			if isSymlink {
				return zipSymlinkError(f.Name)
			}
			dest, err := securePath(stripped)
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
			werr := writeFile(root, dest, rc, f.Mode())
			rc.Close()
			if werr != nil {
				return werr
			}
		}
	}

	if err := missingExtractsError(wanted, found); err != nil {
		return err
	}
	return nil
}

// zipSymlinkError reports an in-scope symlink entry, which this extractor does
// not materialize (unlike the tar one). Entries outside the extraction scope
// are skipped, not reported: an unrelated link elsewhere in the archive must
// not fail an extraction that never touches it.
func zipSymlinkError(name string) error {
	return fmt.Errorf("entry %q is a symlink: not supported", name)
}
