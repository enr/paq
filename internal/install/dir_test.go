package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enr/paq/internal/archive"
)

// zipWithFile writes a zip holding a single file under topDir and returns its path.
func zipWithFile(t *testing.T, topDir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(path, makeFakeZip(topDir, name, content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestInstallDirSwapsAndLeavesNoTemporaries verifies the happy path of the
// swap: the new tree replaces the old one and neither the .tmp staging dir nor
// the .bak backup survives. A backup left behind silently doubles the disk
// usage of every directory install.
func TestInstallDirSwapsAndLeavesNoTemporaries(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "tool")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old-file"), []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	src := zipWithFile(t, "tool-2.0.0", "bin/tool", []byte("v2"))
	if err := InstallDir(src, "zip", dest, archive.ExtractOpts{StripComponents: 1}); err != nil {
		t.Fatalf("InstallDir: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatalf("new tree not in place: %v", err)
	}
	if string(data) != "v2" {
		t.Errorf("content = %q, want v2", data)
	}
	if _, err := os.Stat(filepath.Join(dest, "old-file")); !os.IsNotExist(err) {
		t.Error("the previous tree should have been replaced, not merged")
	}
	for _, suffix := range []string{".tmp", ".bak"} {
		if _, err := os.Stat(dest + suffix); !os.IsNotExist(err) {
			t.Errorf("%s should not survive a successful install, stat err = %v", dest+suffix, err)
		}
	}
}

// TestInstallDirKeepsDestWhenExtractionFails verifies that a corrupt archive
// leaves the previous install untouched: extraction happens into .tmp and the
// swap is only reached once it succeeded.
func TestInstallDirKeepsDestWhenExtractionFails(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "tool")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dest, "keep-me")
	if err := os.WriteFile(marker, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	corrupt := filepath.Join(t.TempDir(), "corrupt.zip")
	if err := os.WriteFile(corrupt, []byte("this is not a zip"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := InstallDir(corrupt, "zip", dest, archive.ExtractOpts{}); err == nil {
		t.Fatal("expected an error for a corrupt archive, got nil")
	}

	if data, err := os.ReadFile(marker); err != nil || string(data) != "v1" {
		t.Errorf("the previous install must survive a failed extraction: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("the staging dir should be cleaned up after a failed extraction, stat err = %v", err)
	}
}

// TestInstallDirClearsLeftoversFromCrashedRun verifies that .tmp/.bak
// directories left behind by an interrupted install are discarded rather than
// extracted into or restored from.
func TestInstallDirClearsLeftoversFromCrashedRun(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "tool")

	for _, suffix := range []string{".tmp", ".bak"} {
		leftover := dest + suffix
		if err := os.MkdirAll(leftover, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(leftover, "stale"), []byte("from a crashed run"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	src := zipWithFile(t, "tool-1.0.0", "bin/tool", []byte("fresh"))
	if err := InstallDir(src, "zip", dest, archive.ExtractOpts{StripComponents: 1}); err != nil {
		t.Fatalf("InstallDir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "stale")); !os.IsNotExist(err) {
		t.Error("a stale .tmp tree was reused instead of being discarded")
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil || string(data) != "fresh" {
		t.Errorf("installed content = %q, err = %v, want fresh", data, err)
	}
	for _, suffix := range []string{".tmp", ".bak"} {
		if _, err := os.Stat(dest + suffix); !os.IsNotExist(err) {
			t.Errorf("%s should not survive, stat err = %v", dest+suffix, err)
		}
	}
}

// TestInstallDirReplacesNonDirectoryDest verifies that a dest which is a plain
// file (a spec changed from a single-binary to a directory layout) is replaced
// rather than causing a failure.
func TestInstallDirReplacesNonDirectoryDest(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(dest, []byte("i used to be a binary"), 0755); err != nil {
		t.Fatal(err)
	}

	src := zipWithFile(t, "tool-1.0.0", "bin/tool", []byte("now a tree"))
	if err := InstallDir(src, "zip", dest, archive.ExtractOpts{StripComponents: 1}); err != nil {
		t.Fatalf("InstallDir over a file dest: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("dest should now be a directory")
	}
	if data, err := os.ReadFile(filepath.Join(dest, "bin", "tool")); err != nil || string(data) != "now a tree" {
		t.Errorf("installed content = %q, err = %v", data, err)
	}
}
