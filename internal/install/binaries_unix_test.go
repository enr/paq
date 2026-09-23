//go:build !windows

// The installed-file permissions are Unix semantics: Windows' os.Chmod only
// toggles the read-only bit, so Mode().Perm() there never reports the mode the
// spec asked for. Windows needs its own expectation (see AUDIT-TESTS.md §8).
package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enr/paq/internal/archive"
)

// TestInstallBinariesAppliesChmod verifies that the spec's chmod reaches every
// installed file, both when they come out of an archive and when the download
// is the bare executable. An installed binary that is not executable is an
// install that reports success and does not work.
func TestInstallBinariesAppliesChmod(t *testing.T) {
	t.Run("from an archive", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "zipp.zip")
		zipData := makeMultiBinZip("zipp-0.8.1_linux_amd64", map[string][]byte{
			"zipts": []byte("ts"),
			"zipls": []byte("ls"),
		})
		if err := os.WriteFile(src, zipData, 0644); err != nil {
			t.Fatal(err)
		}

		destDir := filepath.Join(t.TempDir(), "bin")
		bins := []ResolvedBinary{{From: "zipts", To: "zipts"}, {From: "zipls", To: "zipls"}}
		installed, err := InstallBinaries(src, "zip", bins, destDir, "0755", archive.ExtractOpts{StripComponents: 1})
		if err != nil {
			t.Fatalf("InstallBinaries: %v", err)
		}
		assertPerm(t, installed, 0755)
	})

	t.Run("bare download", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "mytool_1.0.0_linux_amd64")
		if err := os.WriteFile(src, []byte("raw-elf"), 0644); err != nil {
			t.Fatal(err)
		}

		destDir := filepath.Join(t.TempDir(), "bin")
		bins := []ResolvedBinary{{From: "mytool_1.0.0_linux_amd64", To: "mytool"}}
		installed, err := InstallBinaries(src, "", bins, destDir, "0755", archive.ExtractOpts{})
		if err != nil {
			t.Fatalf("InstallBinaries (bare): %v", err)
		}
		assertPerm(t, installed, 0755)
	})

	t.Run("bare download without chmod is still executable", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "mytool_1.0.0_linux_amd64")
		if err := os.WriteFile(src, []byte("raw-elf"), 0644); err != nil {
			t.Fatal(err)
		}

		destDir := filepath.Join(t.TempDir(), "bin")
		bins := []ResolvedBinary{{From: "mytool_1.0.0_linux_amd64", To: "mytool"}}
		installed, err := InstallBinaries(src, "", bins, destDir, "", archive.ExtractOpts{})
		if err != nil {
			t.Fatalf("InstallBinaries (bare): %v", err)
		}
		assertPerm(t, installed, 0755)
	})
}

func assertPerm(t *testing.T, paths []string, want os.FileMode) {
	t.Helper()
	if len(paths) == 0 {
		t.Fatal("no installed paths to check")
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", p, got, want)
		}
	}
}
