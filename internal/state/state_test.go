package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStateLoadSaveRoundtrip(t *testing.T) {
	// Override del path usando XDG_STATE_HOME
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Packages) != 0 {
		t.Error("expected empty state on first load")
	}

	s.Set(InstalledApp{
		Name:        "rg",
		Version:     "14.1.1",
		Kind:        "file",
		Dest:        "/home/u/bin/rg",
		Source:      "https://example.com/rg.tar.gz",
		SHA256:      "abc123",
		InstalledAt: time.Now().UTC().Truncate(time.Second),
	})

	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Verify that the file exists.
	path := filepath.Join(dir, "paq", "state.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not found: %v", err)
	}

	// Reload and verify.
	s2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := s2.Get("rg", "14.1.1")
	if !ok {
		t.Fatal("rg not found after reload")
	}
	if rec.Kind != "file" {
		t.Errorf("kind = %q, want file", rec.Kind)
	}

	// Delete (versione specifica)
	if n := s2.Delete("rg", "14.1.1"); n != 1 {
		t.Errorf("Delete removed %d, want 1", n)
	}
	if err := s2.Save(); err != nil {
		t.Fatal(err)
	}

	s3, _ := Load()
	if _, ok := s3.Get("rg", "14.1.1"); ok {
		t.Error("rg should have been deleted")
	}
}

// TestConcurrentUpdate verifies that parallel Update calls do not lose records.
func TestConcurrentUpdate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		i := i
		go func() {
			defer wg.Done()
			err := Update(func(st *State) error {
				st.Set(InstalledApp{
					Name:    fmt.Sprintf("app%d", i),
					Version: "1.0.0",
					Kind:    "file",
					Dest:    fmt.Sprintf("/usr/bin/app%d", i),
				})
				return nil
			})
			if err != nil {
				t.Errorf("Update error: %v", err)
			}
		}()
	}
	wg.Wait()

	st, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Packages) != n {
		t.Errorf("got %d packages, want %d — concurrent Update lost records", len(st.Packages), n)
	}
}

// TestUpdateFailsWhenLockedByAnotherProcess verifies that a pre-existing
// lock file (simulating another paq process holding it) makes Update fail
// after the timeout with a message naming the lock file.
func TestUpdateFailsWhenLockedByAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	prevInterval, prevTimeout := lockRetryInterval, lockTimeout
	lockRetryInterval = 5 * time.Millisecond
	lockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { lockRetryInterval, lockTimeout = prevInterval, prevTimeout })

	path, err := StatePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, []byte("99999\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = Update(func(st *State) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "locked by another paq process") {
		t.Fatalf("expected lock error, got %v", err)
	}
	if !strings.Contains(err.Error(), lockPath) {
		t.Errorf("error = %q, want it to name the lock file %q", err, lockPath)
	}
}

// TestUpdateLeavesNoLockFileOnSuccess verifies the happy path removes the
// lock file it created.
func TestUpdateLeavesNoLockFileOnSuccess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	if err := Update(func(st *State) error {
		st.Set(InstalledApp{Name: "rg", Version: "1.0.0", Kind: "file", Dest: "/bin/rg"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	path, err := StatePath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Errorf("lock file should have been removed, stat err = %v", err)
	}
}

func TestMultipleVersionsCoexist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	s, _ := Load()
	s.Set(InstalledApp{Name: "jdk", Version: "21.0.2", Kind: "dir", Dest: "/opt/jdk-21.0.2"})
	s.Set(InstalledApp{Name: "jdk", Version: "26", Kind: "dir", Dest: "/opt/jdk-26"})

	if len(s.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(s.Packages))
	}

	byName := s.ByName("jdk")
	if len(byName) != 2 {
		t.Errorf("ByName(jdk) = %d, want 2", len(byName))
	}

	// Upsert: reinstallare la stessa versione non duplica
	s.Set(InstalledApp{Name: "jdk", Version: "21.0.2", Kind: "dir", Dest: "/opt/jdk-21.0.2", SHA256: "new"})
	if len(s.Packages) != 2 {
		t.Errorf("after upsert: expected 2 packages, got %d", len(s.Packages))
	}
	if rec, _ := s.Get("jdk", "21.0.2"); rec.SHA256 != "new" {
		t.Errorf("upsert did not replace record")
	}

	// Delete with no version removes all versions.
	if n := s.Delete("jdk", ""); n != 2 {
		t.Errorf("Delete all removed %d, want 2", n)
	}
	if len(s.Packages) != 0 {
		t.Errorf("expected 0 packages after delete-all, got %d", len(s.Packages))
	}
}
