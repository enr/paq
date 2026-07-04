package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

// mu serializes load-modify-save sequences so that concurrent goroutines
// (e.g. parallel install) do not overwrite each other's state records.
var mu sync.Mutex

// schemaVersion is the current version of the state file format.
const schemaVersion = 2

// InstalledApp records the data of an installed app.
// An entry's identity is the (Name, Version) pair: this allows tracking
// multiple versions of the same app coexisting on disk.
type InstalledApp struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"` // "file", "dir" or "binaries"
	Dest    string `json:"dest"`
	// Files lists the installed paths when Kind == "binaries" (dest is the bin dir).
	Files       []string  `json:"files,omitempty"`
	Source      string    `json:"source"`
	SHA256      string    `json:"sha256"`
	InstalledAt time.Time `json:"installed_at"`
}

// State is paq's state database.
// Packages is a list of installed packages (lockfile convention).
type State struct {
	Schema   int            `json:"schema"`
	Packages []InstalledApp `json:"packages"`
}

// StatePath returns the path of the state.json file.
// On Linux/macOS: ${XDG_STATE_HOME:-~/.local/state}/paq/state.json
// On Windows: %LOCALAPPDATA%\paq\state.json
func StatePath() (string, error) {
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		return filepath.Join(local, "paq", "state.json"), nil
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "paq", "state.json"), nil
}

// Load loads the state from disk.
// Returns an empty State (without error) if the file doesn't exist.
func Load() (*State, error) {
	path, err := StatePath()
	if err != nil {
		return emptyState(), nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return emptyState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.Packages == nil {
		s.Packages = []InstalledApp{}
	}
	return &s, nil
}

// Save saves the state to disk, creating the directory if necessary.
func (s *State) Save() error {
	path, err := StatePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	s.Schema = schemaVersion
	s.sort()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Atomic write: write to temp then rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write state temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// Set upserts an entry: if the (Name, Version) pair already exists it is
// replaced, otherwise it is appended.
func (s *State) Set(rec InstalledApp) {
	for i := range s.Packages {
		if s.Packages[i].Name == rec.Name && s.Packages[i].Version == rec.Version {
			s.Packages[i] = rec
			return
		}
	}
	s.Packages = append(s.Packages, rec)
}

// Get returns the entry for (name, version) if present.
func (s *State) Get(name, version string) (InstalledApp, bool) {
	for _, rec := range s.Packages {
		if rec.Name == name && rec.Version == version {
			return rec, true
		}
	}
	return InstalledApp{}, false
}

// ByName returns all entries (every installed version) for the given name.
func (s *State) ByName(name string) []InstalledApp {
	var out []InstalledApp
	for _, rec := range s.Packages {
		if rec.Name == name {
			out = append(out, rec)
		}
	}
	return out
}

// Delete removes the matching entries.
// If version is empty, removes all versions with that name.
// Returns the number of entries removed.
func (s *State) Delete(name, version string) int {
	kept := s.Packages[:0]
	removed := 0
	for _, rec := range s.Packages {
		match := rec.Name == name && (version == "" || rec.Version == version)
		if match {
			removed++
			continue
		}
		kept = append(kept, rec)
	}
	s.Packages = kept
	return removed
}

// sort orders the entries by name then version, for deterministic output.
func (s *State) sort() {
	sort.Slice(s.Packages, func(i, j int) bool {
		if s.Packages[i].Name != s.Packages[j].Name {
			return s.Packages[i].Name < s.Packages[j].Name
		}
		return s.Packages[i].Version < s.Packages[j].Version
	})
}

// lockRetryInterval and lockTimeout control how long Update waits for another
// process's lock to clear. Vars (not consts) so tests can shrink the timeout.
var (
	lockRetryInterval = 100 * time.Millisecond
	lockTimeout       = 5 * time.Second
)

// Update runs fn inside a mutex (for goroutines of this process) and a
// cross-process lock file (for concurrent paq invocations), preventing a
// load-modify-save race from losing a record.
func Update(fn func(*State) error) error {
	mu.Lock()
	defer mu.Unlock()
	return lockedUpdate(fn)
}

// lockedUpdate acquires the cross-process lock file, then runs the
// load-modify-save sequence. Callers must already hold mu.
func lockedUpdate(fn func(*State) error) error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	lockPath := path + ".lock"

	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			break
		}
		if !os.IsExist(err) {
			return fmt.Errorf("create lock file %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("state is locked by another paq process (remove %s if stale)", lockPath)
		}
		time.Sleep(lockRetryInterval)
	}
	defer os.Remove(lockPath)

	st, err := Load()
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	return st.Save()
}

func emptyState() *State {
	return &State{Schema: schemaVersion, Packages: []InstalledApp{}}
}
