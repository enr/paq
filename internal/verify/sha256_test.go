package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFile(t *testing.T) {
	content := []byte("hello paq")
	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])

	tmp, _ := os.CreateTemp(t.TempDir(), "check-*")
	tmp.Write(content)
	tmp.Close()

	// correct hash
	if err := CheckFile(tmp.Name(), expected); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// wrong hash
	if err := CheckFile(tmp.Name(), "deadbeef"); err == nil {
		t.Error("expected error for wrong hash, got nil")
	}
}

func TestParseSHA256File(t *testing.T) {
	content := fakeSHA256 + "  ripgrep-14.1.1-x86_64-unknown-linux-gnu.tar.gz\n" +
		otherFakeSHA256 + " *ripgrep-14.1.1-x86_64-unknown-linux-gnu.tar.gz.sig\n"

	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha256")
	os.WriteFile(f, []byte(content), 0644)

	hash, err := ParseSHA256File(f, "ripgrep-14.1.1-x86_64-unknown-linux-gnu.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if hash != fakeSHA256 {
		t.Errorf("hash = %q, want %s", hash, fakeSHA256)
	}

	_, err = ParseSHA256File(f, "nonexistent.tar.gz")
	if err == nil {
		t.Error("expected error for missing filename")
	}
}

// TestParseSHA256FileMalformedLine checks that a checksum line whose hash is
// not a sha256 digest is reported as a malformed checksum file, instead of
// being passed on and surfacing later as an artifact mismatch.
func TestParseSHA256FileMalformedLine(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha256")
	os.WriteFile(f, []byte("abc123def456  tool.tar.gz\nnot-a-hash  other.tar.gz\n"), 0644)

	_, err := ParseSHA256File(f, "tool.tar.gz")
	if err == nil {
		t.Fatal("expected an error for a malformed hash")
	}
	if !strings.Contains(err.Error(), "malformed checksum file") {
		t.Errorf("err = %v, want it to mention a malformed checksum file", err)
	}
}

// fakeSHA256 and otherFakeSHA256 are valid-length (64 hex chars) fake sha256
// digests, for fixtures.
const (
	fakeSHA256      = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	otherFakeSHA256 = "1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f809"
)

func TestParseSHA256FileBareHash(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "tool.tar.gz.sha256")
	os.WriteFile(f, []byte(fakeSHA256+"\n"), 0644)

	hash, err := ParseSHA256File(f, "tool.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if hash != fakeSHA256 {
		t.Errorf("hash = %q, want %q", hash, fakeSHA256)
	}
}

func TestParseSHA256FileOneFieldLineDoesNotShortCircuit(t *testing.T) {
	// A stray one-field line precedes the real "hash  filename" line: the
	// named hash must win, not the bare one.
	content := "deadbeef\n" + fakeSHA256 + "  tool.tar.gz\n"

	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha256")
	os.WriteFile(f, []byte(content), 0644)

	hash, err := ParseSHA256File(f, "tool.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if hash != fakeSHA256 {
		t.Errorf("hash = %q, want %q", hash, fakeSHA256)
	}
}

func TestParseSHA256FileMalformedBareHash(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.sha256")
	os.WriteFile(f, []byte("not-a-hash\n"), 0644)

	if _, err := ParseSHA256File(f, "tool.tar.gz"); err == nil {
		t.Error("expected error for malformed bare hash")
	}
}

func TestParseSHA256FileNameNotFound(t *testing.T) {
	content := fakeSHA256 + "  tool.tar.gz\n"

	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha256")
	os.WriteFile(f, []byte(content), 0644)

	if _, err := ParseSHA256File(f, "nonexistent.tar.gz"); err == nil {
		t.Error("expected error for missing filename")
	}
}
