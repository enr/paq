package verify

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CheckFile computes the SHA256 of filePath and compares it with expected (hex string).
// Returns a descriptive error if they don't match.
func CheckFile(filePath string, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", filePath, err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	expected = strings.TrimSpace(strings.ToLower(expected))
	if got != expected {
		return fmt.Errorf("sha256 mismatch for %s:\n  got:  %s\n  want: %s", filepath.Base(filePath), got, expected)
	}
	return nil
}

// ParseSHA256File reads a sha256 checksum file and returns the hash for fileName.
// Supports two formats:
//   - bare hash: the file contains only the hash (layout used by Oracle JDK);
//   - "<hash>  <filename>" / "<hash> *<filename>": line with file name (coreutils layout).
func ParseSHA256File(checksumPath string, fileName string) (string, error) {
	f, err := os.Open(checksumPath)
	if err != nil {
		return "", fmt.Errorf("open checksum file %s: %w", checksumPath, err)
	}
	defer f.Close()

	wantBase := filepath.Base(fileName)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		switch {
		case len(parts) == 1:
			// Bare hash: a single column, the hash refers to the only artifact.
			return strings.ToLower(parts[0]), nil
		default:
			name := strings.TrimPrefix(parts[1], "*")
			if filepath.Base(name) == wantBase {
				return strings.ToLower(parts[0]), nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksum file: %w", err)
	}

	return "", fmt.Errorf("hash for %q not found in %s", wantBase, checksumPath)
}
