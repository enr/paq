package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
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

// makeTarXz creates an in-memory .tar.xz with the given entries.
func makeTarXz(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	xzw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(xzw)
	for name, content := range entries {
		tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		})
		tw.Write([]byte(content))
	}
	tw.Close()
	xzw.Close()

	tmp, _ := os.CreateTemp(t.TempDir(), "test-*.tar.xz")
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

	// "other" must not be present
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
	// Pins the offending entry and the layer that refused it (securePath's
	// lexical check), so the guard's own message stays under test rather
	// than relying only on os.Root's incidental refusal.
	if !strings.Contains(err.Error(), `illegal path "../../evil.txt"`) {
		t.Errorf("error = %v, want it to name the offending entry as illegal", err)
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

// makeTarGzWithHardlink builds an archive containing one regular file plus a
// hardlink entry pointing at it (as found in rootfs tarballs, e.g. terminfo).
func makeTarGzWithHardlink(t *testing.T, files map[string]string, name, target string) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, content := range files {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0644, Size: int64(len(content))})
		tw.Write([]byte(content))
	}
	tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeLink,
		Linkname: target,
		Mode:     0644,
	})
	tw.Close()
	gz.Close()

	tmp, _ := os.CreateTemp(t.TempDir(), "test-hardlink-*.tar.gz")
	tmp.Write(buf.Bytes())
	tmp.Close()
	return tmp.Name()
}

// TestExtractTarGzHardlinkOutsideExtractScopeIgnored verifies that a hardlink
// entry the extraction would never materialize does not fail the install.
func TestExtractTarGzHardlinkOutsideExtractScopeIgnored(t *testing.T) {
	a := makeTarGzWithHardlink(t, map[string]string{
		"app-1.0/tool":                  "binary-content",
		"app-1.0/rootfs/terminfo/v/vt1": "terminfo",
	}, "app-1.0/rootfs/terminfo/v/vt200", "app-1.0/rootfs/terminfo/v/vt1")

	dest := t.TempDir()
	if err := Extract(a, "tar.gz", ExtractOpts{Extract: "tool", Dest: dest}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "tool"))
	if err != nil {
		t.Fatalf("tool not extracted: %v", err)
	}
	if string(data) != "binary-content" {
		t.Errorf("tool content = %q, want %q", data, "binary-content")
	}
}

// TestExtractTarGzHardlinkInScopeRejected verifies that a hardlink that is
// actually part of what gets extracted is still reported as unsupported.
func TestExtractTarGzHardlinkInScopeRejected(t *testing.T) {
	a := makeTarGzWithHardlink(t, map[string]string{
		"app-1.0/tool-orig": "binary-content",
	}, "app-1.0/tool", "app-1.0/tool-orig")

	err := Extract(a, "tar.gz", ExtractOpts{Extract: "tool", Dest: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "hardlink") {
		t.Errorf("error = %v, want hardlink not supported", err)
	}

	err = Extract(a, "tar.gz", ExtractOpts{StripComponents: 1, Dest: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "hardlink") {
		t.Errorf("standard mode: error = %v, want hardlink not supported", err)
	}
}

// makeZipWithSymlink creates a zip containing the given regular files plus a
// symlink entry, stored the way Unix zippers do (mode in the external attrs,
// target as the entry's content).
func makeZipWithSymlink(t *testing.T, files map[string]string, name, target string) string {
	t.Helper()
	tmp, _ := os.CreateTemp(t.TempDir(), "test-symlink-*.zip")
	zw := zip.NewWriter(tmp)
	for fname, content := range files {
		h := &zip.FileHeader{Name: fname}
		h.SetMode(0755)
		f, _ := zw.CreateHeader(h)
		f.Write([]byte(content))
	}
	h := &zip.FileHeader{Name: name}
	h.SetMode(os.ModeSymlink | 0777)
	f, _ := zw.CreateHeader(h)
	f.Write([]byte(target))
	zw.Close()
	tmp.Close()
	return tmp.Name()
}

// TestExtractZipSymlinkOutsideScopeIgnored: a symlink the extraction never
// touches must not fail it. The check used to run before the scope filter, so
// an unrelated link anywhere in the archive broke extracting a single file.
func TestExtractZipSymlinkOutsideScopeIgnored(t *testing.T) {
	z := makeZipWithSymlink(t, map[string]string{
		"tool": "binary",
	}, "docs/latest", "v1.2.3")

	dest := t.TempDir()
	if err := Extract(z, "zip", ExtractOpts{Extract: "tool", Dest: dest}); err != nil {
		t.Fatalf("out-of-scope symlink must not fail the extraction: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary" {
		t.Errorf("extracted content = %q, want binary", string(data))
	}
}

// TestExtractZipSymlinkInScopeRejected: the zip extractor does not materialize
// symlinks, so one it is actually asked to extract stays an error.
func TestExtractZipSymlinkInScopeRejected(t *testing.T) {
	z := makeZipWithSymlink(t, map[string]string{"tool": "binary"}, "link", "tool")

	err := Extract(z, "zip", ExtractOpts{Dest: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error for an in-scope symlink, got nil")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q, want mention of a symlink", err)
	}
}

// TestExtractTarGzExtractNamedSymlinkReported: asking for a file that turns
// out to be a symlink must say so. It used to be skipped, and then reported as
// "not found in archive" — pointing at the wrong problem.
func TestExtractTarGzExtractNamedSymlinkReported(t *testing.T) {
	tgz := makeTarGzWithSymlink(t, map[string]string{
		"tool-1.2.3": "binary",
	}, "tool", "tool-1.2.3")

	err := Extract(tgz, "tar.gz", ExtractOpts{Extract: "tool", Dest: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error for a wanted entry that is a symlink, got nil")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q, want mention of a symlink", err)
	}
	if strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, must not claim the entry is missing", err)
	}
}

// The tar.xz path shares extractTar with tar.gz, so the traversal and symlink
// hardening proven above applies to it too. What is specific to this format is
// the xz decompression wrapper and its routing in Extract: these tests cover
// that seam, plus one traversal case to prove the shared checks are really
// reached through it.

func TestExtractTarXzSingleFile(t *testing.T) {
	txz := makeTarXz(t, map[string]string{
		"micro-2.0.14/micro":   "binary-content",
		"micro-2.0.14/LICENSE": "license",
	})

	dest := t.TempDir()
	if err := Extract(txz, "tar.xz", ExtractOpts{Extract: "micro", Dest: dest}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "micro"))
	if err != nil {
		t.Fatalf("micro not extracted: %v", err)
	}
	if string(data) != "binary-content" {
		t.Errorf("micro content = %q, want binary-content", data)
	}
	if _, err := os.Stat(filepath.Join(dest, "LICENSE")); !os.IsNotExist(err) {
		t.Error("LICENSE should not have been extracted")
	}
}

func TestExtractTarXzStripComponents(t *testing.T) {
	txz := makeTarXz(t, map[string]string{
		"node-v20.11.0/bin/node": "node-binary",
	})

	dest := t.TempDir()
	if err := Extract(txz, "tar.xz", ExtractOpts{StripComponents: 1, Dest: dest}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "node"))
	if err != nil {
		t.Fatalf("bin/node not extracted: %v", err)
	}
	if string(data) != "node-binary" {
		t.Errorf("node content = %q, want node-binary", data)
	}
}

func TestExtractTarXzPathTraversalRejected(t *testing.T) {
	txz := makeTarXz(t, map[string]string{
		"../../evil.txt": "pwned",
	})

	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	if err := Extract(txz, "tar.xz", ExtractOpts{Dest: dest}); err == nil {
		t.Fatal("expected error for path traversal entry, got nil")
	}

	if _, err := os.Stat(filepath.Join(parent, "evil.txt")); !os.IsNotExist(err) {
		t.Error("path traversal entry escaped the destination directory")
	}
}

// A file that is not xz-compressed must fail in the reader, not be mistaken
// for an empty archive that silently extracts nothing.
func TestExtractTarXzRejectsNonXzInput(t *testing.T) {
	notXz := filepath.Join(t.TempDir(), "broken.tar.xz")
	if err := os.WriteFile(notXz, []byte("this is not an xz stream"), 0644); err != nil {
		t.Fatal(err)
	}

	err := Extract(notXz, "tar.xz", ExtractOpts{Dest: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error for a non-xz file, got nil")
	}
	if !strings.Contains(err.Error(), "xz reader") {
		t.Errorf("error = %v, want it to name the xz reader", err)
	}
}

// makeTarZst creates an in-memory .tar.zst with the given entries.
func makeTarZst(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for name, content := range entries {
		tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		})
		tw.Write([]byte(content))
	}
	tw.Close()
	zw.Close()

	tmp, _ := os.CreateTemp(t.TempDir(), "test-*.tar.zst")
	tmp.Write(buf.Bytes())
	tmp.Close()
	return tmp.Name()
}

func TestExtractTarZstSingleFile(t *testing.T) {
	tzst := makeTarZst(t, map[string]string{
		"tool-1.0/tool":    "zst-binary",
		"tool-1.0/LICENSE": "license",
	})

	dest := t.TempDir()
	if err := Extract(tzst, "tar.zst", ExtractOpts{Extract: "tool", Dest: dest}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "tool"))
	if err != nil {
		t.Fatalf("tool not extracted: %v", err)
	}
	if string(data) != "zst-binary" {
		t.Errorf("tool content = %q, want zst-binary", data)
	}
	if _, err := os.Stat(filepath.Join(dest, "LICENSE")); !os.IsNotExist(err) {
		t.Error("LICENSE should not have been extracted")
	}
}

func TestExtractTarZstRejectsNonZstInput(t *testing.T) {
	notZst := filepath.Join(t.TempDir(), "broken.tar.zst")
	if err := os.WriteFile(notZst, []byte("this is not a zstd stream"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(notZst, "tar.zst", ExtractOpts{Dest: t.TempDir()}); err == nil {
		t.Fatal("expected an error for a non-zstd file, got nil")
	}
}

// tarBz2Fixture is a .tar.bz2 (the standard library has no bzip2 writer)
// holding tool-1.0/tool ("bz2-binary") and tool-1.0/LICENSE ("license").
const tarBz2Fixture = "QlpoOTFBWSZTWUz8D+QAAKD/gMuAAIBAA/qACiVIAHolnjAICCAAchpQNBoAA9IAzSBVJAg0NGmINGgA/W5ayd8kDKqIhDdb5XKyi+xWzoQkJRqU3qFqRFypMQxxnQqYKYmuk+z6VjYw0OdJsYahvAHD697UcbkJSvIioJCW8LFs6PDargRAfi7kinChIJn4H8g="

func TestExtractTarBz2StripComponents(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(tarBz2Fixture)
	if err != nil {
		t.Fatal(err)
	}
	tbz := filepath.Join(t.TempDir(), "tool.tar.bz2")
	if err := os.WriteFile(tbz, data, 0644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := Extract(tbz, "tar.bz2", ExtractOpts{StripComponents: 1, Dest: dest}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "tool"))
	if err != nil {
		t.Fatalf("tool not extracted: %v", err)
	}
	if string(got) != "bz2-binary" {
		t.Errorf("tool content = %q, want bz2-binary", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "LICENSE")); err != nil {
		t.Errorf("LICENSE not extracted: %v", err)
	}
}

// "gz" is a single compressed file: asking Extract to unpack it as an
// archive must fail with a message pointing at binaries, not "unsupported".
func TestExtractGzIsNotAnArchive(t *testing.T) {
	err := Extract(filepath.Join(t.TempDir(), "tool.gz"), "gz", ExtractOpts{Dest: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "binaries") {
		t.Fatalf("error = %v, want it to point at binaries", err)
	}
}

func TestGunzip(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("gz-binary"))
	gz.Close()
	src := filepath.Join(t.TempDir(), "tool.gz")
	if err := os.WriteFile(src, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := Gunzip(src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "gz-binary" {
		t.Errorf("content = %q, want gz-binary", got)
	}
}
