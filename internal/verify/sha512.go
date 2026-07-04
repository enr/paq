package verify

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CheckFileSHA512 computes the SHA512 of filePath and compares it with expected (hex string).
// Returns a descriptive error if they don't match.
func CheckFileSHA512(filePath string, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", filePath, err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	expected = strings.TrimSpace(strings.ToLower(expected))
	if got != expected {
		return fmt.Errorf("sha512 mismatch for %s:\n  got:  %s\n  want: %s", filepath.Base(filePath), got, expected)
	}
	return nil
}

// ParseSHA512File reads a sha512 checksum file and returns the hash for fileName.
// Supports two formats:
//   - bare hash: the file contains exactly one line with a single field, the
//     hash refers to the only artifact (layout used by Apache Maven);
//   - "<hash>  <filename>" / "<hash> *<filename>": line with file name (coreutils layout).
//
// Bare-hash mode only applies when the file has exactly one non-empty,
// non-comment line; a stray one-field line among several is skipped instead
// of being mistaken for the whole file's hash.
func ParseSHA512File(checksumPath string, fileName string) (string, error) {
	lines, err := readChecksumLines(checksumPath)
	if err != nil {
		return "", err
	}

	if len(lines) == 1 && len(strings.Fields(lines[0])) == 1 {
		hash := strings.ToLower(strings.Fields(lines[0])[0])
		if len(hash) != 128 || !isHex(hash) {
			return "", fmt.Errorf("malformed checksum file %s", checksumPath)
		}
		return hash, nil
	}

	wantBase := filepath.Base(fileName)
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimPrefix(parts[1], "*")
		if filepath.Base(name) == wantBase {
			return strings.ToLower(parts[0]), nil
		}
	}

	return "", fmt.Errorf("hash for %q not found in %s", wantBase, checksumPath)
}
