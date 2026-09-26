package install

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	}
	// The permissions applied to the installed files are Unix semantics:
	// asserted by TestInstallBinariesAppliesChmod (binaries_unix_test.go).
}

// TestInstallBinariesSameFromUnderSeveralNames verifies that one archive entry
// can be installed under more than one name (e.g. an alias): extraction
// yields a single file, so every name but one must get its own copy.
func TestInstallBinariesSameFromUnderSeveralNames(t *testing.T) {
	src := filepath.Join(t.TempDir(), "tool.zip")
	if err := os.WriteFile(src, makeMultiBinZip("tool-1.0.0", map[string][]byte{"tool": []byte("bin")}), 0644); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(t.TempDir(), "bin")
	bins := []ResolvedBinary{{From: "tool", To: "tool"}, {From: "tool", To: "t"}}
	installed, err := InstallBinaries(src, "zip", bins, destDir, "0755", archive.ExtractOpts{StripComponents: 1})
	if err != nil {
		t.Fatalf("InstallBinaries: %v", err)
	}
	if len(installed) != 2 {
		t.Fatalf("installed = %v, want 2 paths", installed)
	}
	for _, name := range []string{"tool", "t"} {
		got, err := os.ReadFile(filepath.Join(destDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != "bin" {
			t.Errorf("%s content = %q, want %q", name, got, "bin")
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
}

// A "gz" artifact is a single compressed executable: it is decompressed and
// installed like a bare download.
func TestInstallBinariesGz(t *testing.T) {
	content := []byte("raw-elf")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(content)
	gz.Close()
	src := filepath.Join(t.TempDir(), "mytool-linux-amd64.gz")
	if err := os.WriteFile(src, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(t.TempDir(), "bin")
	bins := []ResolvedBinary{{To: "mytool"}}

	if _, err := InstallBinaries(src, "gz", bins, destDir, "", archive.ExtractOpts{}); err != nil {
		t.Fatalf("InstallBinaries (gz) failed: %v", err)
	}

	dest := filepath.Join(destDir, "mytool")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read mytool: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
	if info, err := os.Stat(dest); err == nil && info.Mode().Perm()&0100 == 0 && runtime.GOOS != "windows" {
		t.Errorf("mode = %v, want executable", info.Mode().Perm())
	}
}

// TestInstallBinariesMissingFromNamesIt verifies that a From that isn't
// present in the archive fails with an error naming it.
func TestInstallBinariesMissingFromNamesIt(t *testing.T) {
	files := map[string][]byte{
		"zipts": []byte("ts"),
	}
	zipData := makeMultiBinZip("zipp-0.8.1_linux_amd64", files)

	src := filepath.Join(t.TempDir(), "zipp.zip")
	if err := os.WriteFile(src, zipData, 0644); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(t.TempDir(), "bin")
	bins := []ResolvedBinary{
		{From: "zipts", To: "zipts"},
		{From: "does-not-exist", To: "missing"},
	}

	_, err := InstallBinaries(src, "zip", bins, destDir, "0755", archive.ExtractOpts{StripComponents: 1})
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("expected error naming does-not-exist, got %v", err)
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
