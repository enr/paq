package archive

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ExtractOpts configures how to extract an archive.
type ExtractOpts struct {
	// StripComponents removes the first N components of each entry's path.
	StripComponents int
	// Extract: if non-empty, extracts only the file with this name (basename
	// only). Equivalent to (and folded into) Extracts = []string{Extract}.
	Extract string
	// Extracts: if non-empty, extracts only the files whose basename is in
	// this set into Dest, once each. Mutually exclusive with Subdir. Missing
	// names cause an error listing them.
	Extracts []string
	// Subdir: if non-empty, extracts only the files whose path (after StripComponents)
	// has this prefix. Supports a glob for the first component (e.g. "*/Contents/Home").
	Subdir string
	// Dest is the destination directory.
	Dest string
}

// Extract picks the extraction method based on archiveType and runs it.
//
// Every write goes through an os.Root anchored at opts.Dest: archives are
// untrusted input, and Root refuses to traverse out of the root, resolving
// symlinks per component in the kernel. That covers what a lexical check on
// the entry path cannot see, such as a link reached through another link.
func Extract(archivePath string, archiveType string, opts ExtractOpts) error {
	if err := os.MkdirAll(opts.Dest, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	root, err := os.OpenRoot(opts.Dest)
	if err != nil {
		return fmt.Errorf("open dest dir: %w", err)
	}
	defer root.Close()

	if opts.Extract != "" && len(opts.Extracts) == 0 {
		opts.Extracts = []string{opts.Extract}
	}

	switch archiveType {
	case "tar.gz", "tgz":
		return extractTarGz(archivePath, root, opts)
	case "tar.xz":
		return extractTarXz(archivePath, root, opts)
	case "zip":
		return extractZip(archivePath, root, opts)
	default:
		return fmt.Errorf("unsupported archive type: %q", archiveType)
	}
}

// extractSet builds a deduplicated lookup set of wanted basenames from
// opts.Extracts. Returns nil if there is nothing to extract by name.
func extractSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// missingExtractsError builds the "not found" error for an extract set,
// given which wanted names were actually found. Returns nil if none are missing.
func missingExtractsError(wanted, found map[string]bool) error {
	var missing []string
	for name := range wanted {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	if len(missing) == 1 {
		return fmt.Errorf("file %q not found in archive", missing[0])
	}
	return fmt.Errorf("files %s not found in archive", strings.Join(missing, ", "))
}

// securePath cleans name (a slash-separated path taken from an archive entry)
// into a path relative to the extraction root, rejecting entries that escape
// it (zip-slip / tar-slip). The os.Root the caller writes through enforces the
// same rule; this check runs first so the error names the offending entry
// instead of surfacing as a generic "path escapes from parent".
func securePath(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal path %q in archive: escapes destination directory", name)
	}
	return clean, nil
}

// writeFile writes the reader's content to name, relative to root, creating
// the necessary directories.
func writeFile(root *os.Root, name string, r io.Reader, mode os.FileMode) error {
	if dir := filepath.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode&0777|0200)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	// Closed explicitly, not deferred: a buffered write that fails on flush
	// (full disk, quota) reports it here and nowhere else, and the extracted
	// file would otherwise be silently truncated.
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	// Apply the correct permissions after writing.
	return root.Chmod(name, mode&0777|0200)
}

// symlinkExtractError reports a wanted entry that is a symlink. Single-file
// and multi-binary installs copy one file out of the archive, with no
// surrounding tree for the link to resolve against. It is reported instead of
// skipped: skipping surfaces later as a misleading "not found in archive".
func symlinkExtractError(name string) error {
	return fmt.Errorf("entry %q is a symlink: not supported when extracting by name", name)
}
