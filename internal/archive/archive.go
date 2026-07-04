package archive

import (
	"fmt"
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
func Extract(archivePath string, archiveType string, opts ExtractOpts) error {
	if err := os.MkdirAll(opts.Dest, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	if opts.Extract != "" && len(opts.Extracts) == 0 {
		opts.Extracts = []string{opts.Extract}
	}

	switch archiveType {
	case "tar.gz", "tgz":
		return extractTarGz(archivePath, opts)
	case "tar.xz":
		return extractTarXz(archivePath, opts)
	case "zip":
		return extractZip(archivePath, opts)
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

// securePath joins name (a slash-separated path taken from an archive entry)
// onto destRoot and verifies the result stays inside destRoot. Archives are
// untrusted input: an entry like "../../etc/passwd" must not be allowed to
// escape the extraction directory (zip-slip / tar-slip).
func securePath(destRoot, name string) (string, error) {
	dest := filepath.Join(destRoot, filepath.FromSlash(name))
	cleanRoot := filepath.Clean(destRoot)
	cleanDest := filepath.Clean(dest)
	if cleanDest != cleanRoot && !strings.HasPrefix(cleanDest, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal path %q in archive: escapes destination directory", name)
	}
	return cleanDest, nil
}
