package archive

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// symlinkEntry is a symlink found in the archive, created after all regular
// files so that no file write can pass through an archive-provided symlink.
type symlinkEntry struct {
	name     string // path, relative to the extraction root, of the symlink
	linkname string // link target as stored in the archive
}

// extractTar extracts a tar archive from reader with the given options.
// This function is shared by tar.gz and tar.xz.
func extractTar(r io.Reader, root *os.Root, opts ExtractOpts) error {
	tr := tar.NewReader(r)

	wanted := extractSet(opts.Extracts)
	found := make(map[string]bool, len(wanted)) // used for Extracts mode
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

		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue // metadata entries, never materialized
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			continue // special files: never useful in a tool archive, skip
		}

		switch {
		case wanted != nil:
			// Extracts mode: look up each wanted file by basename.
			base := filepath.Base(stripped)
			if !wanted[base] {
				continue
			}
			if hdr.Typeflag == tar.TypeLink {
				return hardlinkError(hdr.Name)
			}
			if hdr.Typeflag == tar.TypeSymlink {
				return symlinkExtractError(hdr.Name)
			}
			if hdr.Typeflag != tar.TypeDir {
				if found[base] {
					return fmt.Errorf("multiple files named %q in archive: ambiguous extract", base)
				}
				name, err := securePath(base)
				if err != nil {
					return err
				}
				if err := writeFile(root, name, tr, hdr.FileInfo().Mode()); err != nil {
					return err
				}
				found[base] = true
			}

		case opts.Subdir != "":
			// Subtree mode: extract only files under subdir (with a glob on the first segment).
			rel, match := matchSubdir(stripped, opts.Subdir)
			if !match || rel == "" {
				continue
			}
			name, err := securePath(rel)
			if err != nil {
				return err
			}
			switch hdr.Typeflag {
			case tar.TypeDir:
				if err := root.MkdirAll(name, 0755); err != nil {
					return err
				}
			case tar.TypeSymlink:
				symlinks = append(symlinks, symlinkEntry{name: name, linkname: hdr.Linkname})
			case tar.TypeReg:
				if err := writeFile(root, name, tr, hdr.FileInfo().Mode()); err != nil {
					return err
				}
			case tar.TypeLink:
				return hardlinkError(hdr.Name)
			default:
				continue
			}

		default:
			// Standard mode: extract everything.
			name, err := securePath(stripped)
			if err != nil {
				return err
			}
			switch hdr.Typeflag {
			case tar.TypeDir:
				if err := root.MkdirAll(name, 0755); err != nil {
					return err
				}
			case tar.TypeSymlink:
				symlinks = append(symlinks, symlinkEntry{name: name, linkname: hdr.Linkname})
			case tar.TypeReg:
				if err := writeFile(root, name, tr, hdr.FileInfo().Mode()); err != nil {
					return err
				}
			case tar.TypeLink:
				return hardlinkError(hdr.Name)
			default:
				continue
			}
		}
	}

	for _, l := range symlinks {
		if err := writeSymlink(root, l.name, l.linkname); err != nil {
			return err
		}
	}
	// Containment is checked only once every link exists: a chain escapes the
	// root as a whole (a -> ".", b -> "a/../..") while each hop looks contained.
	for _, l := range symlinks {
		if err := checkSymlinkResolves(root, l.name); err != nil {
			return err
		}
	}

	if err := missingExtractsError(wanted, found); err != nil {
		return err
	}
	return nil
}

// hardlinkError reports an in-scope hardlink entry, which extraction cannot
// materialize. Entries outside the extraction scope are skipped, not reported.
func hardlinkError(name string) error {
	return fmt.Errorf("entry %q is a hardlink: not supported", name)
}

// writeSymlink creates the symlink name (relative to root) pointing at
// linkname, after verifying that the target stays inside root. Absolute
// targets and relative targets that resolve outside root are rejected without
// touching the filesystem: archives are untrusted input.
func writeSymlink(root *os.Root, name, linkname string) error {
	if filepath.IsAbs(linkname) {
		return fmt.Errorf("symlink %q has absolute target %q: not supported", name, linkname)
	}
	if _, err := securePath(filepath.Join(filepath.Dir(name), filepath.FromSlash(linkname))); err != nil {
		return fmt.Errorf("symlink target %q escapes destination directory", linkname)
	}
	if dir := filepath.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	// Remove any existing file so re-installs over an old tree don't fail.
	if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	if err := root.Symlink(filepath.FromSlash(linkname), name); err != nil {
		return fmt.Errorf("symlink %s: %w", name, err)
	}
	return nil
}

// checkSymlinkResolves verifies that name, followed through every symlink on
// its path, still lands inside root. os.Root resolves each component in the
// kernel, so this sees what the lexical check in writeSymlink cannot: a link
// that only escapes once another link on its path exists. A target that simply
// does not exist is fine, since archives may legitimately ship dangling links.
func checkSymlinkResolves(root *os.Root, name string) error {
	_, err := root.Stat(name)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	// Do not leave a rejected link behind for the caller to clean up.
	root.Remove(name)
	return fmt.Errorf("symlink %q does not resolve inside the destination directory: %w", name, err)
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
