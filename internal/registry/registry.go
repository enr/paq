// Package registry manages the local cache of the external registry
// snapshot: the recipe files downloaded by "paq registry update" that
// overlay the registry embedded in the binary.
package registry

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// metaSchema is the current version of the meta.json format.
const metaSchema = 1

// metaFile is the metadata file stored inside the snapshot directory.
const metaFile = "meta.json"

// Meta describes an installed external registry snapshot.
type Meta struct {
	Schema int `json:"schema"`
	// Tag is the release tag the snapshot was downloaded from
	// (or "custom" for a user-configured URL).
	Tag string `json:"tag"`
	// Version is the registry version read from the archive's VERSION file.
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetched_at"`
	SourceURL string    `json:"source_url"`
	SpecCount int       `json:"spec_count"`
}

// Dir returns the directory holding the external registry snapshot.
// On Linux/macOS: ${XDG_CACHE_HOME:-~/.cache}/paq/registry
// On Windows: %LOCALAPPDATA%\paq\cache\registry
func Dir() (string, error) {
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		return filepath.Join(local, "paq", "cache", "registry"), nil
	}

	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		cacheHome = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheHome, "paq", "registry"), nil
}

// Open returns the snapshot as an fs.FS (rooted so that recipes are under
// "registry/") together with its metadata.
// Returns (nil, nil, nil) when no snapshot is installed; an error when a
// snapshot directory exists but its metadata is missing or corrupt.
func Open() (fs.FS, *Meta, error) {
	dir, err := Dir()
	if err != nil {
		return nil, nil, err
	}

	data, err := os.ReadFile(filepath.Join(dir, metaFile))
	if os.IsNotExist(err) {
		if _, serr := os.Stat(dir); os.IsNotExist(serr) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("registry cache %s has no %s", dir, metaFile)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read registry cache metadata: %w", err)
	}

	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, nil, fmt.Errorf("parse registry cache metadata: %w", err)
	}

	return os.DirFS(dir), &meta, nil
}

// StagingDir creates a temporary directory next to the snapshot directory
// (same filesystem, so the final rename in Install is atomic). The caller
// is responsible for removing it on failure.
func StagingDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	return os.MkdirTemp(parent, "registry.staging-")
}

// Install atomically replaces the snapshot with the contents of stagingDir
// (created via StagingDir, containing the extracted registry/ subdir).
// On success stagingDir no longer exists; on failure the previous snapshot,
// if any, is restored.
func Install(stagingDir string, meta Meta) error {
	dir, err := Dir()
	if err != nil {
		return err
	}

	meta.Schema = metaSchema
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, metaFile), data, 0644); err != nil {
		return fmt.Errorf("write registry metadata: %w", err)
	}

	// Three-step swap: move the current snapshot aside, move the staging in
	// place, then drop the old one. A leftover .old from a crashed run is
	// removed first.
	old := dir + ".old"
	if err := os.RemoveAll(old); err != nil {
		return fmt.Errorf("remove stale registry backup: %w", err)
	}
	hadPrevious := false
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return fmt.Errorf("move current registry aside: %w", err)
		}
		hadPrevious = true
	}
	if err := os.Rename(stagingDir, dir); err != nil {
		if hadPrevious {
			os.Rename(old, dir) // restore the previous snapshot
		}
		return fmt.Errorf("install registry snapshot: %w", err)
	}
	if hadPrevious {
		os.RemoveAll(old)
	}
	return nil
}
