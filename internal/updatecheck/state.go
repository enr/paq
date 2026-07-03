// Package updatecheck manages the small on-disk record used by the passive
// "a new paq version is available" notice: when paq last checked GitHub for a
// release and the latest version it found.
package updatecheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// schema is the current version of the update-check.json format.
const schema = 1

// State is the persisted update-check record.
type State struct {
	Schema        int       `json:"schema"`
	LastChecked   time.Time `json:"last_checked"`
	LatestVersion string    `json:"latest_version"`
	LatestTag     string    `json:"latest_tag"`
}

// Path returns the path of the update-check.json file.
// On Linux/macOS: ${XDG_CACHE_HOME:-~/.cache}/paq/update-check.json
// On Windows: %LOCALAPPDATA%\paq\cache\update-check.json
func Path() (string, error) {
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		return filepath.Join(local, "paq", "cache", "update-check.json"), nil
	}

	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		cacheHome = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheHome, "paq", "update-check.json"), nil
}

// Load reads the update-check record from disk.
// Returns a zero State (without error) if the file doesn't exist.
func Load() (State, error) {
	path, err := Path()
	if err != nil {
		return State{}, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read update-check %s: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse update-check: %w", err)
	}
	return s, nil
}

// Save writes the update-check record to disk, creating the directory if
// necessary. The write is atomic (temp file + rename).
func (s State) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	s.Schema = schema
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal update-check: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write update-check temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename update-check: %w", err)
	}
	return nil
}
