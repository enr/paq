package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// makeTarGz creates an in-memory .tar.gz with the given entries.
// entries is a path→content map.
func makeTarGz(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		})
		tw.Write([]byte(content))
	}
	tw.Close()
	gz.Close()

	tmp, _ := os.CreateTemp(t.TempDir(), "test-*.tar.gz")
	tmp.Write(buf.Bytes())
	tmp.Close()
	return tmp.Name()
}

func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	tmp, _ := os.CreateTemp(t.TempDir(), "test-*.zip")
	zw := zip.NewWriter(tmp)
	for name, content := range entries {
		f, _ := zw.Create(name)
		f.Write([]byte(content))
	}
	zw.Close()
	tmp.Close()
	return tmp.Name()
}

func TestExtractTarGzSingleFile(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"ripgrep-14.1.1-x86_64-unknown-linux-gnu/rg":     "binary-content",
		"ripgrep-14.1.1-x86_64-unknown-linux-gnu/README": "readme",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{
		Extract: "rg",
		Dest:    dest,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "rg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary-content" {
		t.Errorf("content = %q, want binary-content", string(data))
	}
}

func TestExtractTarGzStripComponents(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"jdk-21.0.2/bin/java":   "java-binary",
		"jdk-21.0.2/lib/rt.jar": "rt-jar",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{
		StripComponents: 1,
		Dest:            dest,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "java"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "java-binary" {
		t.Errorf("java content = %q, want java-binary", string(data))
	}
}

func TestExtractTarGzSubdir(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"jdk-21.jdk/Contents/Home/bin/java": "java-binary",
		"jdk-21.jdk/Contents/Home/lib/foo":  "lib-foo",
		"jdk-21.jdk/other/file":             "other",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{
		Subdir: "*/Contents/Home",
		Dest:   dest,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "java"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "java-binary" {
		t.Errorf("java content = %q, want java-binary", string(data))
	}

	// "other" non deve essere presente
	if _, err := os.Stat(filepath.Join(dest, "other")); !os.IsNotExist(err) {
		t.Error("'other' should not have been extracted")
	}
}

func TestExtractZipSingleFile(t *testing.T) {
	z := makeZip(t, map[string]string{
		"dir/rg.exe": "exe-content",
		"dir/README": "readme",
	})

	dest := t.TempDir()
	err := Extract(z, "zip", ExtractOpts{
		Extract: "rg.exe",
		Dest:    dest,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "rg.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "exe-content" {
		t.Errorf("content = %q, want exe-content", string(data))
	}
}

func TestExtractMissingFile(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"dir/other": "content",
	})
	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{Extract: "rg", Dest: dest})
	if err == nil {
		t.Error("expected error for missing extract file")
	}
}

func TestExtractZipPathTraversalRejected(t *testing.T) {
	z := makeZip(t, map[string]string{
		"../../evil.txt": "pwned",
	})

	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	err := Extract(z, "zip", ExtractOpts{Dest: dest})
	if err == nil {
		t.Fatal("expected error for path traversal entry, got nil")
	}

	if _, statErr := os.Stat(filepath.Join(parent, "evil.txt")); !os.IsNotExist(statErr) {
		t.Error("path traversal entry escaped the destination directory")
	}
}

func TestExtractTarGzPathTraversalRejected(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"../../evil.txt": "pwned",
	})

	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	err := Extract(tgz, "tar.gz", ExtractOpts{Dest: dest})
	if err == nil {
		t.Fatal("expected error for path traversal entry, got nil")
	}

	if _, statErr := os.Stat(filepath.Join(parent, "evil.txt")); !os.IsNotExist(statErr) {
		t.Error("path traversal entry escaped the destination directory")
	}
}

// makeTarGzWithSymlink creates a .tar.gz containing a single symlink entry
// pointing at target, used to verify symlink entries are rejected.
func makeTarGzWithSymlink(t *testing.T, name, target string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeSymlink,
		Linkname: target,
		Mode:     0777,
	})
	tw.Close()
	gz.Close()

	tmp, _ := os.CreateTemp(t.TempDir(), "test-symlink-*.tar.gz")
	tmp.Write(buf.Bytes())
	tmp.Close()
	return tmp.Name()
}

func TestExtractTarGzSymlinkRejected(t *testing.T) {
	tgz := makeTarGzWithSymlink(t, "evil-link", "/etc/passwd")

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{Dest: dest})
	if err == nil {
		t.Fatal("expected error for symlink entry, got nil")
	}

	if _, statErr := os.Stat(filepath.Join(dest, "evil-link")); !os.IsNotExist(statErr) {
		t.Error("symlink entry should not have been extracted")
	}
}
