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

// schemaVersion è la versione corrente del formato dello state file.
const schemaVersion = 2

// InstalledApp registra i dati di un'app installata.
// L'identità di una entry è la coppia (Name, Version): questo consente di
// tracciare più versioni della stessa app coesistenti su disco.
type InstalledApp struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"` // "file", "dir" o "binaries"
	Dest    string `json:"dest"`
	// Files elenca i path installati quando Kind == "binaries" (dest è la bin dir).
	Files       []string  `json:"files,omitempty"`
	Source      string    `json:"source"`
	SHA256      string    `json:"sha256"`
	InstalledAt time.Time `json:"installed_at"`
}

// State è il database di stato di paq.
// Packages è una lista di pacchetti installati (convenzione lockfile).
type State struct {
	Schema   int         `json:"schema"`
	Packages []InstalledApp `json:"packages"`
}

// StatePath ritorna il path del file state.json.
// Su Linux/macOS: ${XDG_STATE_HOME:-~/.local/state}/paq/state.json
// Su Windows: %LOCALAPPDATA%\paq\state.json
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

// Load carica lo state dal disco.
// Ritorna uno State vuoto (senza errore) se il file non esiste.
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

// Save salva lo state sul disco, creando la directory se necessaria.
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

	// Scrittura atomica: scrivi in temp poi rename
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

// Set fa upsert di una entry: se esiste già la coppia (Name, Version) la sostituisce,
// altrimenti la aggiunge.
func (s *State) Set(rec InstalledApp) {
	for i := range s.Packages {
		if s.Packages[i].Name == rec.Name && s.Packages[i].Version == rec.Version {
			s.Packages[i] = rec
			return
		}
	}
	s.Packages = append(s.Packages, rec)
}

// Get ritorna la entry per (name, version) se presente.
func (s *State) Get(name, version string) (InstalledApp, bool) {
	for _, rec := range s.Packages {
		if rec.Name == name && rec.Version == version {
			return rec, true
		}
	}
	return InstalledApp{}, false
}

// ByName ritorna tutte le entry (ogni versione installata) per il nome dato.
func (s *State) ByName(name string) []InstalledApp {
	var out []InstalledApp
	for _, rec := range s.Packages {
		if rec.Name == name {
			out = append(out, rec)
		}
	}
	return out
}

// Delete rimuove le entry corrispondenti.
// Se version è vuoto, rimuove tutte le versioni con quel nome.
// Ritorna il numero di entry rimosse.
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

// sort ordina le entry per nome poi versione, per output deterministico.
func (s *State) sort() {
	sort.Slice(s.Packages, func(i, j int) bool {
		if s.Packages[i].Name != s.Packages[j].Name {
			return s.Packages[i].Name < s.Packages[j].Name
		}
		return s.Packages[i].Version < s.Packages[j].Version
	})
}

// Update runs fn inside a mutex that serializes the load-modify-save
// sequence, preventing concurrent goroutines from clobbering each other.
func Update(fn func(*State) error) error {
	mu.Lock()
	defer mu.Unlock()
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
