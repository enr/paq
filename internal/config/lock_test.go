package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// isolateManifest points userConfigPath (and thus LockPath) at a fresh temp
// file for the duration of the test, restoring PathOverride afterwards.
func isolateManifest(t *testing.T) string {
	t.Helper()
	saved := PathOverride
	t.Cleanup(func() { PathOverride = saved })
	PathOverride = filepath.Join(t.TempDir(), "config.toml")
	return PathOverride
}

func TestLockPathIsNextToManifest(t *testing.T) {
	cfgPath := isolateManifest(t)

	got, err := LockPath()
	if err != nil {
		t.Fatalf("LockPath: %v", err)
	}
	want := filepath.Join(filepath.Dir(cfgPath), "paq.lock.toml")
	if got != want {
		t.Errorf("LockPath = %q, want %q", got, want)
	}
}

func TestLoadLockMissingFileReturnsEmpty(t *testing.T) {
	isolateManifest(t)

	lock, err := LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock == nil || lock.Apps == nil {
		t.Fatal("LoadLock returned a nil Lock or nil Apps map for a missing file")
	}
	if len(lock.Apps) != 0 {
		t.Errorf("LoadLock.Apps = %v, want empty", lock.Apps)
	}
}

func TestWriteLockEntryThenLoadRoundTrips(t *testing.T) {
	isolateManifest(t)

	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1", SHA256: "abc123"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}

	lock, err := LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	entry, ok := lock.Apps["rg"]
	if !ok {
		t.Fatal(`LoadLock.Apps["rg"] missing after WriteLockEntry`)
	}
	if entry.Version != "14.1.1" || entry.SHA256 != "abc123" {
		t.Errorf("entry = %+v, want {Version:14.1.1 SHA256:abc123}", entry)
	}
}

func TestWriteLockEntryOverwritesExisting(t *testing.T) {
	isolateManifest(t)

	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	if err := WriteLockEntry("rg", LockEntry{Version: "14.2.0"}); err != nil {
		t.Fatalf("WriteLockEntry (overwrite): %v", err)
	}

	lock, err := LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if got := lock.Apps["rg"].Version; got != "14.2.0" {
		t.Errorf("version = %q, want %q", got, "14.2.0")
	}
}

func TestWriteLockEntryPreservesOtherApps(t *testing.T) {
	isolateManifest(t)

	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	if err := WriteLockEntry("bat", LockEntry{Version: "0.24.0"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}

	lock, err := LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if len(lock.Apps) != 2 {
		t.Fatalf("Apps = %v, want 2 entries", lock.Apps)
	}
	if lock.Apps["rg"].Version != "14.1.1" || lock.Apps["bat"].Version != "0.24.0" {
		t.Errorf("Apps = %+v, want rg=14.1.1 and bat=0.24.0 both present", lock.Apps)
	}
}

// TestWriteLockEntryConcurrentWritersKeepEveryEntry guards the parallel
// install/upgrade path, where several goroutines pin their app at once: an
// unserialized load-modify-save lets the last writer drop the others' entries.
func TestWriteLockEntryConcurrentWritersKeepEveryEntry(t *testing.T) {
	isolateManifest(t)

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- WriteLockEntry(fmt.Sprintf("app%d", i), LockEntry{Version: "1.0.0"})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("WriteLockEntry: %v", err)
		}
	}

	lock, err := LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if len(lock.Apps) != writers {
		t.Errorf("Apps = %v, want %d entries", lock.Apps, writers)
	}
}

func TestDeleteLockEntry(t *testing.T) {
	isolateManifest(t)

	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	if err := WriteLockEntry("bat", LockEntry{Version: "0.24.0"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	if err := DeleteLockEntry("rg"); err != nil {
		t.Fatalf("DeleteLockEntry: %v", err)
	}

	lock, err := LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if _, ok := lock.Apps["rg"]; ok {
		t.Error(`Apps["rg"] still present after DeleteLockEntry`)
	}
	if _, ok := lock.Apps["bat"]; !ok {
		t.Error(`Apps["bat"] dropped by DeleteLockEntry("rg")`)
	}
}

// DeleteLockEntry must be a no-op, not an error, both for an unknown app and
// for a lockfile that doesn't exist yet — mirrors how the rest of the package
// treats "nothing to do" as success.
func TestDeleteLockEntryNoOpOnMissingEntryOrFile(t *testing.T) {
	isolateManifest(t)

	if err := DeleteLockEntry("rg"); err != nil {
		t.Errorf("DeleteLockEntry on a missing lockfile: %v, want nil", err)
	}

	if err := WriteLockEntry("bat", LockEntry{Version: "0.24.0"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	if err := DeleteLockEntry("rg"); err != nil {
		t.Errorf("DeleteLockEntry on an unknown entry: %v, want nil", err)
	}
}

// The lockfile is meant to be committed to version control, so rewriting it
// must not shuffle entries: two writes of the same set of apps (regardless of
// insertion order) must produce byte-identical files.
func TestSaveLockIsDeterministicallyOrdered(t *testing.T) {
	isolateManifest(t)

	if err := WriteLockEntry("zipp", LockEntry{Version: "1.0.0"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	if err := WriteLockEntry("bat", LockEntry{Version: "0.24.0"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1"}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}

	path, err := LockPath()
	if err != nil {
		t.Fatalf("LockPath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	firstIdx := func(name string) int {
		return strings.Index(string(data), "[apps."+name+"]")
	}
	batIdx, rgIdx, zippIdx := firstIdx("bat"), firstIdx("rg"), firstIdx("zipp")
	if !(batIdx < rgIdx && rgIdx < zippIdx) {
		t.Errorf("lockfile entries not alphabetically ordered:\n%s", data)
	}
}
