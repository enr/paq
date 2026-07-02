package install

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/enr/paq/internal/archive"
)

// makeMultiBinZip creates a zip with multiple files under a top-level dir.
func makeMultiBinZip(topDir string, files map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, _ := zw.Create(topDir + "/" + name)
		w.Write(content)
	}
	zw.Close()
	return buf.Bytes()
}

func TestInstallBinaries(t *testing.T) {
	files := map[string][]byte{
		"zipts": []byte("ts"),
		"zipls": []byte("ls"),
		"zipw":  []byte("w"),
	}
	zipData := makeMultiBinZip("zipp-0.8.1_linux_amd64", files)

	src := filepath.Join(t.TempDir(), "zipp.zip")
	if err := os.WriteFile(src, zipData, 0644); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(t.TempDir(), "bin")
	bins := []ResolvedBinary{
		{From: "zipts", To: "zipts"},
		{From: "zipls", To: "zipls"},
		{From: "zipw", To: "zipwatch"}, // rename
	}

	installed, err := InstallBinaries(src, "zip", bins, destDir, "0755", archive.ExtractOpts{StripComponents: 1})
	if err != nil {
		t.Fatalf("InstallBinaries failed: %v", err)
	}
	if len(installed) != 3 {
		t.Fatalf("installed len = %d, want 3", len(installed))
	}

	checks := map[string][]byte{
		"zipts":    files["zipts"],
		"zipls":    files["zipls"],
		"zipwatch": files["zipw"],
	}
	for name, want := range checks {
		got, err := os.ReadFile(filepath.Join(destDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s content = %q, want %q", name, got, want)
		}
		info, err := os.Stat(filepath.Join(destDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0755 {
			t.Errorf("%s mode = %o, want 0755", name, info.Mode().Perm())
		}
	}
}

// TestInstallBinariesBare verifies the case with no archive: the downloaded
// artifact is the executable (name with os/arch in the filename) and must be
// installed under a clean name.
func TestInstallBinariesBare(t *testing.T) {
	content := []byte("raw-elf")
	src := filepath.Join(t.TempDir(), "mytool_1.0.0_linux_amd64")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(t.TempDir(), "bin")
	bins := []ResolvedBinary{{From: "mytool_1.0.0_linux_amd64", To: "mytool"}}

	installed, err := InstallBinaries(src, "", bins, destDir, "0755", archive.ExtractOpts{})
	if err != nil {
		t.Fatalf("InstallBinaries (bare) failed: %v", err)
	}
	if len(installed) != 1 {
		t.Fatalf("installed len = %d, want 1", len(installed))
	}

	got, err := os.ReadFile(filepath.Join(destDir, "mytool"))
	if err != nil {
		t.Fatalf("read mytool: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
	info, err := os.Stat(filepath.Join(destDir, "mytool"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("mode = %o, want 0755", info.Mode().Perm())
	}
}

// TestInstallBinariesBareRejectsMultiple verifies that a bare download only
// accepts a single entry.
func TestInstallBinariesBareRejectsMultiple(t *testing.T) {
	src := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	bins := []ResolvedBinary{{To: "a"}, {To: "b"}}
	if _, err := InstallBinaries(src, "", bins, filepath.Join(t.TempDir(), "bin"), "0755", archive.ExtractOpts{}); err == nil {
		t.Fatal("expected error for multiple bare binaries, got nil")
	}
}
