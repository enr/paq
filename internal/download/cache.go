package download

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CacheDir returns the download cache directory, set by PAQ_DOWNLOAD_CACHE.
// "" means the cache is off: it is opt-in, meant for a directory a CI system
// keeps between builds (a CodeBuild or GitHub Actions cache path, a Docker
// cache mount).
func CacheDir() string {
	return strings.TrimSpace(os.Getenv("PAQ_DOWNLOAD_CACHE"))
}

// FromCache copies the archive cached in dir under its sha256 to a temp file
// and returns that file's path; the caller removes it after use, like a
// ToTemp result. It reports false when nothing is cached under that hash, or
// when the cached file no longer matches it (a corrupt entry is not reused).
func FromCache(dir, sum string) (string, bool) {
	sum = strings.ToLower(sum)
	if !validSum(sum) {
		return "", false
	}
	src, err := os.Open(filepath.Join(dir, sum))
	if err != nil {
		return "", false
	}
	defer src.Close()

	tmp, err := os.CreateTemp("", "paq-download-*")
	if err != nil {
		return "", false
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil || hex.EncodeToString(h.Sum(nil)) != sum {
		os.Remove(tmp.Name())
		return "", false
	}
	return tmp.Name(), true
}

// ToCache stores the file at path in dir under sum, its sha256. The copy goes
// through a temp file and a rename, so a concurrent reader never sees a
// partial entry.
func ToCache(dir, path, sum string) error {
	sum = strings.ToLower(sum)
	if !validSum(sum) {
		return fmt.Errorf("invalid sha256 %q", sum)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create download cache: %w", err)
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("write download cache: %w", err)
	}
	_, err = io.Copy(tmp, src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(dir, sum))
	}
	if err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write download cache: %w", err)
	}
	return nil
}

// validSum reports whether s is a lowercase hex sha256 digest, which also
// keeps it a plain file name inside the cache directory.
func validSum(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
