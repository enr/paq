package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1", Tag: "14.1.1", SHA256ByPlatform: map[string]string{"linux-amd64-gnu": "abc123", "darwin-arm64": "def456"}}); err != nil {
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
	want := LockEntry{Version: "14.1.1", Tag: "14.1.1", SHA256ByPlatform: map[string]string{"linux-amd64-gnu": "abc123", "darwin-arm64": "def456"}}
	if !reflect.DeepEqual(entry, want) {
		t.Errorf("entry = %+v, want %+v", entry, want)
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

// TestSaveLockWritesPlatformHashesSorted pins the on-disk layout: one
// [apps.<name>.sha256] table per app, platforms sorted for a stable diff.
func TestSaveLockWritesPlatformHashesSorted(t *testing.T) {
	isolateManifest(t)

	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1", Tag: "14.1.1", SHA256ByPlatform: map[string]string{"linux-amd64-gnu": "bbb", "darwin-arm64": "aaa"}}); err != nil {
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
	want := `
[apps.rg]
version = "14.1.1"
tag = "14.1.1"

[apps.rg.sha256]
darwin-arm64 = "aaa"
linux-amd64-gnu = "bbb"
`
	if !strings.HasSuffix(string(data), want) {
		t.Errorf("lockfile =\n%s\nwant it to end with:\n%s", data, want)
	}
}

// TestLoadLockDropsLegacyPlatformlessSHA256 covers lockfiles written before
// 0.4.0, whose sha256 is one string with no platform: it must still load,
// keeping the version and tag, with no hash for any platform.
func TestLoadLockDropsLegacyPlatformlessSHA256(t *testing.T) {
	isolateManifest(t)

	path, err := LockPath()
	if err != nil {
		t.Fatalf("LockPath: %v", err)
	}
	legacy := "[apps.rg]\nversion = \"14.1.1\"\ntag = \"14.1.1\"\nsha256 = \"abc123\"\n"
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	lock, err := LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	want := LockEntry{Version: "14.1.1", Tag: "14.1.1"}
	if got := lock.Apps["rg"]; !reflect.DeepEqual(got, want) {
		t.Errorf("entry = %+v, want %+v", got, want)
	}
}

func TestLoadLockRejectsNonStringPlatformHash(t *testing.T) {
	isolateManifest(t)

	path, err := LockPath()
	if err != nil {
		t.Fatalf("LockPath: %v", err)
	}
	bad := "[apps.rg]\nversion = \"14.1.1\"\n\n[apps.rg.sha256]\nlinux-amd64-gnu = 42\n"
	if err := os.WriteFile(path, []byte(bad), 0644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	if _, err := LoadLock(); err == nil || !strings.Contains(err.Error(), "apps.rg.sha256.linux-amd64-gnu") {
		t.Errorf("LoadLock error = %v, want one naming apps.rg.sha256.linux-amd64-gnu", err)
	}
}

// TestWriteLockEntryMergesPlatformHashes: another platform installing the
// same release adds its hash next to the existing ones.
func TestWriteLockEntryMergesPlatformHashes(t *testing.T) {
	isolateManifest(t)

	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1", Tag: "14.1.1", SHA256ByPlatform: map[string]string{"linux-amd64-gnu": "aaa"}}); err != nil {
		t.Fatalf("WriteLockEntry: %v", err)
	}
	if err := WriteLockEntry("rg", LockEntry{Version: "14.1.1", Tag: "14.1.1", SHA256ByPlatform: map[string]string{"darwin-arm64": "bbb"}}); err != nil {
		t.Fatalf("WriteLockEntry (second platform): %v", err)
	}

	lock, err := LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	want := map[string]string{"linux-amd64-gnu": "aaa", "darwin-arm64": "bbb"}
	if got := lock.Apps["rg"].SHA256ByPlatform; !reflect.DeepEqual(got, want) {
		t.Errorf("hashes = %v, want %v", got, want)
	}
}

// TestWriteLockEntryNewReleaseDropsOtherPlatformHashes: hashes recorded for
// the previous release (or another build under the same version, i.e. a
// different tag) describe other archives, so they must not survive.
func TestWriteLockEntryNewReleaseDropsOtherPlatformHashes(t *testing.T) {
	cases := []struct {
		name     string
		old, new LockEntry
	}{
		{
			"new version",
			LockEntry{Version: "14.1.1", Tag: "14.1.1", SHA256ByPlatform: map[string]string{"darwin-arm64": "old"}},
			LockEntry{Version: "14.2.0", Tag: "14.2.0", SHA256ByPlatform: map[string]string{"linux-amd64-gnu": "new"}},
		},
		{
			"same version, new tag",
			LockEntry{Version: "21.0.12", Tag: "jdk-21.0.12+8", SHA256ByPlatform: map[string]string{"darwin-arm64": "old"}},
			LockEntry{Version: "21.0.12", Tag: "jdk-21.0.12+9", SHA256ByPlatform: map[string]string{"linux-amd64-gnu": "new"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateManifest(t)

			if err := WriteLockEntry("rg", tc.old); err != nil {
				t.Fatalf("WriteLockEntry (old): %v", err)
			}
			if err := WriteLockEntry("rg", tc.new); err != nil {
				t.Fatalf("WriteLockEntry (new): %v", err)
			}

			lock, err := LoadLock()
			if err != nil {
				t.Fatalf("LoadLock: %v", err)
			}
			if got := lock.Apps["rg"]; !reflect.DeepEqual(got, tc.new) {
				t.Errorf("entry = %+v, want %+v", got, tc.new)
			}
		})
	}
}
