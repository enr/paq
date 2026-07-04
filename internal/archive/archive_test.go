package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
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

// TestExtractTarGzAmbiguousNameRejected verifies that Extract fails loudly
// when two entries share the wanted basename, instead of silently letting
// the last match win.
func TestExtractTarGzAmbiguousNameRejected(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"bin/rg":   "bin-content",
		"debug/rg": "debug-content",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{Extract: "rg", Dest: dest})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected an ambiguous-extract error, got %v", err)
	}
}

// TestExtractZipSkipsDirectoryEntryMatchingBasename verifies that a
// directory entry named "rg" doesn't win over a real file, and doesn't
// produce an empty output file.
func TestExtractZipSkipsDirectoryEntryMatchingBasename(t *testing.T) {
	tmp, _ := os.CreateTemp(t.TempDir(), "test-zip-dir-*.zip")
	zw := zip.NewWriter(tmp)
	// Explicit directory entry named "rg/".
	_, err := zw.CreateHeader(&zip.FileHeader{Name: "rg/", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := zw.Create("sub/rg")
	f.Write([]byte("real-content"))
	zw.Close()
	tmp.Close()

	dest := t.TempDir()
	if err := Extract(tmp.Name(), "zip", ExtractOpts{Extract: "rg", Dest: dest}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "rg"))
	if err != nil {
		t.Fatalf("extracted file not found: %v", err)
	}
	if string(data) != "real-content" {
		t.Errorf("content = %q, want real-content", string(data))
	}
}

// TestExtractMultipleNamesSinglePass verifies that ExtractOpts.Extracts pulls
// several files out of the archive in one pass, and that a missing name is
// reported by name.
func TestExtractMultipleNamesSinglePass(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"bin/rg":  "rg-content",
		"bin/bat": "bat-content",
		"bin/fd":  "fd-content",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{Extracts: []string{"rg", "bat"}, Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"rg": "rg-content", "bat": "bat-content"} {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Fatalf("%s not extracted: %v", name, err)
		}
		if string(data) != want {
			t.Errorf("%s content = %q, want %q", name, data, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "fd")); !os.IsNotExist(err) {
		t.Error("fd should not have been extracted (not in Extracts)")
	}
}

func TestExtractMultipleNamesMissingReportsName(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"bin/rg": "rg-content",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{Extracts: []string{"rg", "missing-tool"}, Dest: dest})
	if err == nil || !strings.Contains(err.Error(), "missing-tool") {
		t.Fatalf("expected error naming missing-tool, got %v", err)
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

// TestExtractTarGzSkipsMetadataAndSpecialEntries verifies that a
// pax_global_header entry (as produced by "git archive") and a FIFO special
// file are skipped rather than materialized as regular files, while the
// real regular file is still extracted.
func TestExtractTarGzSkipsMetadataAndSpecialEntries(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{
		Name:     "pax_global_header",
		Typeflag: tar.TypeXGlobalHeader,
		Size:     0,
	})
	tw.WriteHeader(&tar.Header{
		Name:     "a-fifo",
		Typeflag: tar.TypeFifo,
		Mode:     0644,
	})
	content := "actual-content"
	tw.WriteHeader(&tar.Header{
		Name: "real-file",
		Mode: 0644,
		Size: int64(len(content)),
	})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()

	tmp, _ := os.CreateTemp(t.TempDir(), "test-metadata-*.tar.gz")
	tmp.Write(buf.Bytes())
	tmp.Close()

	dest := t.TempDir()
	if err := Extract(tmp.Name(), "tar.gz", ExtractOpts{StripComponents: 0, Dest: dest}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dest, "pax_global_header")); !os.IsNotExist(err) {
		t.Error("pax_global_header should not have been extracted")
	}
	if _, err := os.Stat(filepath.Join(dest, "a-fifo")); !os.IsNotExist(err) {
		t.Error("FIFO entry should not have been extracted")
	}
	data, err := os.ReadFile(filepath.Join(dest, "real-file"))
	if err != nil {
		t.Fatalf("real-file not extracted: %v", err)
	}
	if string(data) != content {
		t.Errorf("real-file content = %q, want %q", data, content)
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

// makeTarGzWithSymlink creates a .tar.gz containing the given regular files
// plus a single symlink entry pointing at target.
func makeTarGzWithSymlink(t *testing.T, files map[string]string, name, target string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for fname, content := range files {
		tw.WriteHeader(&tar.Header{
			Name: fname,
			Mode: 0755,
			Size: int64(len(content)),
		})
		tw.Write([]byte(content))
	}
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

func TestExtractTarGzSymlink(t *testing.T) {
	// Node-style layout: bin/npm is a symlink into lib/.
	tgz := makeTarGzWithSymlink(t, map[string]string{
		"node-v24/bin/node":                            "node-binary",
		"node-v24/lib/node_modules/npm/bin/npm-cli.js": "npm-cli",
	}, "node-v24/bin/npm", "../lib/node_modules/npm/bin/npm-cli.js")

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{StripComponents: 1, Dest: dest})
	if err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dest, "bin", "npm")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.FromSlash("../lib/node_modules/npm/bin/npm-cli.js") {
		t.Errorf("link target = %q", target)
	}
	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "npm-cli" {
		t.Errorf("content through symlink = %q, want npm-cli", string(data))
	}
}

func TestExtractTarGzSymlinkAbsoluteTargetRejected(t *testing.T) {
	tgz := makeTarGzWithSymlink(t, nil, "evil-link", "/etc/passwd")

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{Dest: dest})
	if err == nil {
		t.Fatal("expected error for symlink with absolute target, got nil")
	}

	if _, statErr := os.Lstat(filepath.Join(dest, "evil-link")); !os.IsNotExist(statErr) {
		t.Error("symlink entry should not have been extracted")
	}
}

func TestExtractTarGzSymlinkEscapingTargetRejected(t *testing.T) {
	tgz := makeTarGzWithSymlink(t, nil, "dir/evil-link", "../../outside")

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{Dest: dest})
	if err == nil {
		t.Fatal("expected error for symlink escaping the destination, got nil")
	}

	if _, statErr := os.Lstat(filepath.Join(dest, "dir", "evil-link")); !os.IsNotExist(statErr) {
		t.Error("symlink entry should not have been extracted")
	}
}
