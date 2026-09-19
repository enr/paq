//go:build !windows

// Symlink extraction is Unix-only in practice: creating a symlink on Windows
// needs a privilege the test runner does not have by default, and "/etc/passwd"
// is not an absolute path there, so the absolute-target refusal cannot be
// triggered the same way. Windows needs its own equivalents (see
// AUDIT-TESTS.md §8). The symlink tests that never touch the filesystem
// (the zip extractor's refusals, the lexically-escaping target, a wanted entry
// that turns out to be a symlink) stay in archive_test.go and run everywhere.
package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestExtractTarGzSymlinkChainEscapeRejected: "sub/up -> .." lands exactly on
// the destination, and "x -> sub/up/../.." is lexically inside it too, because
// cleaning cancels "up/.." as if up were a directory. It is not: following the
// links, x resolves two levels above the destination. Only resolving the path
// per component catches this.
func TestExtractTarGzSymlinkChainEscapeRejected(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "sub/", Typeflag: tar.TypeDir, Mode: 0755})
	for _, l := range []struct{ name, target string }{
		{"sub/up", ".."},
		{"x", "sub/up/../.."},
	} {
		tw.WriteHeader(&tar.Header{
			Name:     l.name,
			Typeflag: tar.TypeSymlink,
			Linkname: l.target,
			Mode:     0777,
		})
	}
	tw.Close()
	gz.Close()
	tgz := filepath.Join(t.TempDir(), "chain.tar.gz")
	if err := os.WriteFile(tgz, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	err := Extract(tgz, "tar.gz", ExtractOpts{Dest: dest})
	if err == nil {
		t.Fatal("expected an error for a symlink chain escaping the destination, got nil")
	}
	if !strings.Contains(err.Error(), "resolve") {
		t.Errorf("error = %q, want mention of the link not resolving inside", err)
	}
	if _, statErr := os.Lstat(filepath.Join(dest, "x")); !os.IsNotExist(statErr) {
		t.Error("the escaping link should not have been left behind")
	}
}

// TestExtractTarGzDanglingSymlinkAllowed: a link whose target is missing is
// contained, just broken. Archives ship those, so it must not be an error.
func TestExtractTarGzDanglingSymlinkAllowed(t *testing.T) {
	tgz := makeTarGzWithSymlink(t, nil, "bin/link", "../lib/not-shipped.so")

	dest := t.TempDir()
	if err := Extract(tgz, "tar.gz", ExtractOpts{Dest: dest}); err != nil {
		t.Fatalf("dangling but contained symlink must be allowed: %v", err)
	}
	target, err := os.Readlink(filepath.Join(dest, "bin", "link"))
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.FromSlash("../lib/not-shipped.so") {
		t.Errorf("link target = %q", target)
	}
}
