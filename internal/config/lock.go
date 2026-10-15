package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// LockEntry is one app's pinned resolution in the lockfile: the version paq
// resolved (with its release tag) the last time it ran a live "latest"
// resolution for that app, and the sha256 of the archive each platform
// installed for it. The tag keeps what the clean version drops, e.g.
// Temurin's build number in "jdk-21.0.12+8". The hashes are per platform
// (keyed by platform.Defaults.Key) because the lockfile is shared across
// machines and each platform downloads a different archive.
type LockEntry struct {
	Version          string
	Tag              string
	SHA256ByPlatform map[string]string
}

// lockEntryFile is a LockEntry as paq.lock.toml stores it. sha256 is a
// table of platform hashes; a plain string is the single, platform-less hash
// written before 0.4.0, which says nothing about this platform and is dropped.
type lockEntryFile struct {
	Version string `toml:"version"`
	Tag     string `toml:"tag"`
	SHA256  any    `toml:"sha256"`
}

// Lock is the parsed content of paq.lock.toml: one LockEntry per app name.
type Lock struct {
	Apps map[string]LockEntry `toml:"apps"`
}

// LockPath returns the path of the lockfile: it always lives next to the
// user manifest (so --config/PAQ_CONFIG relocate both together).
func LockPath() (string, error) {
	cfgPath, err := userConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfgPath), "paq.lock.toml"), nil
}

// LoadLock loads the lockfile. A missing file is not an error: it returns an
// empty Lock, exactly like LoadUserConfig treats a missing manifest — most
// manifests have no lockfile yet, and that's normal, not broken.
func LoadLock() (*Lock, error) {
	path, err := LockPath()
	if err != nil {
		return &Lock{Apps: map[string]LockEntry{}}, nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{Apps: map[string]LockEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read lockfile %s: %w", path, err)
	}

	var file struct {
		Apps map[string]lockEntryFile `toml:"apps"`
	}
	if err := toml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse lockfile %s: %w", path, err)
	}
	lock := &Lock{Apps: map[string]LockEntry{}}
	for name, e := range file.Apps {
		entry := LockEntry{Version: e.Version, Tag: e.Tag}
		if sums, ok := e.SHA256.(map[string]any); ok {
			entry.SHA256ByPlatform = map[string]string{}
			for plat, sum := range sums {
				s, ok := sum.(string)
				if !ok {
					return nil, fmt.Errorf("parse lockfile %s: apps.%s.sha256.%s is not a string", path, name, plat)
				}
				entry.SHA256ByPlatform[plat] = s
			}
		}
		lock.Apps[name] = entry
	}
	return lock, nil
}

// lockMu serializes the lockfile's load-modify-save sequences: a parallel
// install/upgrade pins several apps concurrently, and unserialized writers
// would each overwrite the others' entries (and race on the shared temp file).
var lockMu sync.Mutex

// WriteLockEntry sets (or overwrites) appName's lock entry and rewrites the
// lockfile. Used after a live "latest" resolution, so a later `paq install`
// (on this machine or another sharing the manifest+lockfile) reproduces the
// same version instead of resolving "latest" again and possibly landing on a
// newer release; and after an install from the lock entry, to add the hash
// of a platform it lacks. The hashes other platforms recorded are kept while
// the version and tag stay the same (they still name the same release), and
// dropped otherwise.
func WriteLockEntry(appName string, entry LockEntry) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	lock, err := LoadLock()
	if err != nil {
		return err
	}
	if old, ok := lock.Apps[appName]; ok && old.Version == entry.Version && old.Tag == entry.Tag {
		sums := maps.Clone(old.SHA256ByPlatform)
		if sums == nil {
			sums = map[string]string{}
		}
		maps.Copy(sums, entry.SHA256ByPlatform)
		entry.SHA256ByPlatform = sums
	}
	lock.Apps[appName] = entry
	return saveLock(lock)
}

// DeleteLockEntry removes appName's lock entry, if any. A no-op (no error)
// when the lockfile doesn't exist or has no entry for it.
func DeleteLockEntry(appName string) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	lock, err := LoadLock()
	if err != nil {
		return err
	}
	if _, ok := lock.Apps[appName]; !ok {
		return nil
	}
	delete(lock.Apps, appName)
	return saveLock(lock)
}

// saveLock writes the lockfile atomically (temp file + rename), with entries
// sorted by app name: the file is meant to be committed to version control,
// and go-toml's map encoding order is not stable, which would otherwise
// produce a spurious diff on every write.
func saveLock(lock *Lock) error {
	path, err := LockPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	names := make([]string, 0, len(lock.Apps))
	for name := range lock.Apps {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# generated by paq — do not edit by hand; refreshed by `paq upgrade`\n")
	for _, name := range names {
		e := lock.Apps[name]
		fmt.Fprintf(&b, "\n[apps.%s]\n", name)
		fmt.Fprintf(&b, "version = %q\n", e.Version)
		if e.Tag != "" {
			fmt.Fprintf(&b, "tag = %q\n", e.Tag)
		}
		if len(e.SHA256ByPlatform) > 0 {
			fmt.Fprintf(&b, "\n[apps.%s.sha256]\n", name)
			for _, plat := range slices.Sorted(maps.Keys(e.SHA256ByPlatform)) {
				fmt.Fprintf(&b, "%s = %q\n", plat, e.SHA256ByPlatform[plat])
			}
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace lockfile: %w", err)
	}
	return nil
}
