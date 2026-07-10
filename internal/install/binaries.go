package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/enr/paq/internal/archive"
)

// ResolvedBinary is an executable to install with templates already resolved.
// From is the basename of the file inside the archive (empty for bare
// downloads); To is the name to install it as.
type ResolvedBinary struct {
	From string
	To   string
}

// InstallBinaries installs one or more executables into destDir, applying
// chmod to each. Returns the absolute paths of the installed files (for the state).
//
// If archiveType is empty, the downloaded artifact IS the executable (bare
// download): exactly one entry is allowed and the artifact is installed as
// destDir/<To>. Otherwise each binary is extracted from the archive (by
// basename From) into a temp dir on the same filesystem as destDir, then
// moved into destDir/<To>.
func InstallBinaries(artifactPath, archiveType string, bins []ResolvedBinary, destDir, chmod string, opts archive.ExtractOpts) ([]string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}

	mode, err := parseFileMode(chmod)
	if err != nil {
		return nil, err
	}

	// Bare download: the artifact is the binary, no extraction.
	if archiveType == "" {
		if len(bins) != 1 {
			return nil, fmt.Errorf("a non-archive download installs exactly one binary, got %d", len(bins))
		}
		dest := filepath.Join(destDir, bins[0].To)
		if err := installRawBinary(artifactPath, dest, mode); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	}

	tmpDir, err := os.MkdirTemp(destDir, "paq-install-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract every binary in a single pass (one decompression regardless of
	// how many binaries the spec lists), then apply chmod to each.
	froms := make(map[string]bool, len(bins))
	for _, b := range bins {
		if b.From == "" {
			return nil, fmt.Errorf("binaries: 'from' is required when 'archive' is set")
		}
		// Extraction matches archive entries by basename, so a 'from' carrying a
		// path can never match: reject it with a clear error instead of a
		// misleading "not found in archive".
		if strings.Contains(b.From, "/") {
			return nil, fmt.Errorf("binaries: 'from' must be a file name, not a path: %q", b.From)
		}
		froms[b.From] = true
	}
	extracts := make([]string, 0, len(froms))
	for from := range froms {
		extracts = append(extracts, from)
	}

	eopts := opts
	eopts.Extracts = extracts
	eopts.Dest = tmpDir
	if err := archive.Extract(artifactPath, archiveType, eopts); err != nil {
		return nil, fmt.Errorf("extract binaries: %w", err)
	}

	for from := range froms {
		if mode != 0 {
			if err := os.Chmod(filepath.Join(tmpDir, from), mode); err != nil {
				return nil, fmt.Errorf("chmod %q: %w", from, err)
			}
		}
	}

	// Move each binary into destDir/<To>.
	var installed []string
	for _, b := range bins {
		extracted := filepath.Join(tmpDir, b.From)
		dest := filepath.Join(destDir, b.To)
		if err := os.Rename(extracted, dest); err != nil {
			return installed, fmt.Errorf("install %q: %w", b.To, err)
		}
		installed = append(installed, dest)
	}

	return installed, nil
}

// installRawBinary copies the artifact to dest with an atomic swap in the
// same dir, applying mode (the artifact may be on a different filesystem than dest).
func installRawBinary(src, dest string, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), "paq-install-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	in, err := os.Open(src)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("open artifact: %w", err)
	}
	_, copyErr := io.Copy(tmp, in)
	in.Close()
	tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("copy artifact: %w", copyErr)
	}

	if mode != 0 {
		if err := os.Chmod(tmpName, mode); err != nil {
			return fmt.Errorf("chmod: %w", err)
		}
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	return nil
}

// parseFileMode parses an octal chmod (e.g. "0755"); "" → 0 (no chmod).
func parseFileMode(chmod string) (os.FileMode, error) {
	if chmod == "" {
		return 0, nil
	}
	m, err := strconv.ParseUint(chmod, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("parse chmod %q: %w", chmod, err)
	}
	return os.FileMode(m), nil
}
