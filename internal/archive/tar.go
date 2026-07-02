package archive

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractTar extracts a tar archive from reader with the given options.
// This function is shared by tar.gz and tar.xz.
func extractTar(r io.Reader, opts ExtractOpts) error {
	tr := tar.NewReader(r)

	found := false // used for Extract mode (single file)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Normalize the path and apply StripComponents.
		name := filepath.ToSlash(hdr.Name)
		name = strings.TrimPrefix(name, "./")
		stripped, ok := stripComponents(name, opts.StripComponents)
		if !ok || stripped == "" {
			continue
		}

		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return fmt.Errorf("entry %q is a symlink/hardlink: not supported", hdr.Name)
		}

		switch {
		case opts.Extract != "":
			// Single-file mode: look up the file by basename.
			if filepath.Base(stripped) == opts.Extract {
				dest, err := securePath(opts.Dest, opts.Extract)
				if err != nil {
					return err
				}
				if err := writeFile(tr, dest, hdr.FileInfo().Mode()); err != nil {
					return err
				}
				found = true
			}

		case opts.Subdir != "":
			// Subtree mode: extract only files under subdir (with a glob on the first segment).
			rel, match := matchSubdir(stripped, opts.Subdir)
			if !match || rel == "" {
				continue
			}
			dest, err := securePath(opts.Dest, rel)
			if err != nil {
				return err
			}
			if hdr.Typeflag == tar.TypeDir {
				if err := os.MkdirAll(dest, 0755); err != nil {
					return err
				}
			} else {
				if err := writeFile(tr, dest, hdr.FileInfo().Mode()); err != nil {
					return err
				}
			}

		default:
			// Standard mode: extract everything.
			dest, err := securePath(opts.Dest, stripped)
			if err != nil {
				return err
			}
			if hdr.Typeflag == tar.TypeDir {
				if err := os.MkdirAll(dest, 0755); err != nil {
					return err
				}
			} else {
				if err := writeFile(tr, dest, hdr.FileInfo().Mode()); err != nil {
					return err
				}
			}
		}
	}

	if opts.Extract != "" && !found {
		return fmt.Errorf("file %q not found in archive", opts.Extract)
	}
	return nil
}

// stripComponents removes the first n components of the path.
// Returns ("", false) if the path has fewer than n components.
func stripComponents(path string, n int) (string, bool) {
	if n <= 0 {
		return path, true
	}
	parts := strings.SplitN(path, "/", n+1)
	if len(parts) <= n {
		return "", false
	}
	return parts[n], true
}

// matchSubdir checks whether path starts with the subdir prefix (which may have
// "*" as a glob in the first segment). On a match, returns the path relative to subdir.
func matchSubdir(path, subdir string) (rel string, ok bool) {
	subdirParts := strings.Split(strings.TrimSuffix(subdir, "/"), "/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) < len(subdirParts) {
		return "", false
	}

	for i, sp := range subdirParts {
		pp := pathParts[i]
		if sp == "*" {
			continue // glob: any segment matches
		}
		if sp != pp {
			return "", false
		}
	}

	rel = strings.Join(pathParts[len(subdirParts):], "/")
	return rel, true
}

// writeFile writes the reader's content to file dest, creating the necessary directories.
func writeFile(r io.Reader, dest string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode&0777|0200)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	// Apply the correct permissions after writing.
	return os.Chmod(dest, mode&0777|0200)
}
