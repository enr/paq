package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtractOpts configures how to extract an archive.
type ExtractOpts struct {
	// StripComponents removes the first N components of each entry's path.
	StripComponents int
	// Extract: if non-empty, extracts only the file with this name (basename only).
	Extract string
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
