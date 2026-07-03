package archive

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// symlinkEntry is a symlink found in the archive, created after all regular
// files so that no file write can pass through an archive-provided symlink.
type symlinkEntry struct {
	dest     string // absolute path where the symlink is created
	linkname string // link target as stored in the archive
}

// extractTar extracts a tar archive from reader with the given options.
// This function is shared by tar.gz and tar.xz.
func extractTar(r io.Reader, opts ExtractOpts) error {
	tr := tar.NewReader(r)

	found := false // used for Extract mode (single file)
	var symlinks []symlinkEntry

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

		if hdr.Typeflag == tar.TypeLink {
			return fmt.Errorf("entry %q is a hardlink: not supported", hdr.Name)
		}

		switch {
		case opts.Extract != "":
			// Single-file mode: look up the file by basename.
			if hdr.Typeflag != tar.TypeSymlink && filepath.Base(stripped) == opts.Extract {
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
			switch hdr.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(dest, 0755); err != nil {
					return err
				}
			case tar.TypeSymlink:
				symlinks = append(symlinks, symlinkEntry{dest: dest, linkname: hdr.Linkname})
			default:
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
			switch hdr.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(dest, 0755); err != nil {
					return err
				}
			case tar.TypeSymlink:
				symlinks = append(symlinks, symlinkEntry{dest: dest, linkname: hdr.Linkname})
			default:
				if err := writeFile(tr, dest, hdr.FileInfo().Mode()); err != nil {
					return err
				}
			}
		}
	}

	for _, l := range symlinks {
		if err := writeSymlink(opts.Dest, l.dest, l.linkname); err != nil {
			return err
		}
	}

	if opts.Extract != "" && !found {
		return fmt.Errorf("file %q not found in archive", opts.Extract)
	}
	return nil
}

// writeSymlink creates a symlink at dest pointing to linkname, after verifying
// that the target stays inside destRoot. Absolute targets and relative targets
// that resolve outside destRoot are rejected: archives are untrusted input.
func writeSymlink(destRoot, dest, linkname string) error {
	if filepath.IsAbs(linkname) {
		return fmt.Errorf("symlink %q has absolute target %q: not supported", dest, linkname)
	}
	target := filepath.Join(filepath.Dir(dest), filepath.FromSlash(linkname))
	cleanRoot := filepath.Clean(destRoot)
	if target != cleanRoot && !strings.HasPrefix(target, cleanRoot+string(os.PathSeparator)) {
		return fmt.Errorf("symlink target %q escapes destination directory", linkname)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
	}
	// Remove any existing file so re-installs over an old tree don't fail.
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", dest, err)
	}
	if err := os.Symlink(filepath.FromSlash(linkname), dest); err != nil {
		return fmt.Errorf("symlink %s: %w", dest, err)
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
