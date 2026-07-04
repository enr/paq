package verify

import (
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckFileSHA512(t *testing.T) {
	content := []byte("hello paq")
	sum := sha512.Sum512(content)
	expected := hex.EncodeToString(sum[:])

	tmp, _ := os.CreateTemp(t.TempDir(), "check-*")
	tmp.Write(content)
	tmp.Close()

	// correct hash
	if err := CheckFileSHA512(tmp.Name(), expected); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// correct hash but with uppercase/spaces (normalization)
	if err := CheckFileSHA512(tmp.Name(), "  "+expected+"\n"); err != nil {
		t.Errorf("expected no error for padded hash, got: %v", err)
	}

	// wrong hash
	if err := CheckFileSHA512(tmp.Name(), "deadbeef"); err == nil {
		t.Error("expected error for wrong hash, got nil")
	}
}

// fakeSHA512 is a valid-length (128 hex chars) fake sha512 digest, for fixtures.
const fakeSHA512 = "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e"

func TestParseSHA512FileBareHash(t *testing.T) {
	// Apache Maven layout: the .sha512 file contains only the hash.
	dir := t.TempDir()
	f := filepath.Join(dir, "apache-maven-3.9.16-bin.zip.sha512")
	os.WriteFile(f, []byte(fakeSHA512+"\n"), 0644)

	hash, err := ParseSHA512File(f, "apache-maven-3.9.16-bin.zip")
	if err != nil {
		t.Fatal(err)
	}
	if hash != fakeSHA512 {
		t.Errorf("hash = %q, want %q", hash, fakeSHA512)
	}
}

func TestParseSHA512FileWithFilename(t *testing.T) {
	content := fakeSHA512 + "  apache-maven-3.9.16-bin.zip\n" +
		fakeSHA512 + " *other-file.zip\n"

	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha512")
	os.WriteFile(f, []byte(content), 0644)

	hash, err := ParseSHA512File(f, "apache-maven-3.9.16-bin.zip")
	if err != nil {
		t.Fatal(err)
	}
	if hash != fakeSHA512 {
		t.Errorf("hash = %q, want %q", hash, fakeSHA512)
	}
}

func TestParseSHA512FileOneFieldLineDoesNotShortCircuit(t *testing.T) {
	// A stray one-field line precedes the real "hash  filename" line: the
	// named hash must win, not the bare one.
	content := "deadbeef\n" +
		fakeSHA512 + "  apache-maven-3.9.16-bin.zip\n"

	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha512")
	os.WriteFile(f, []byte(content), 0644)

	hash, err := ParseSHA512File(f, "apache-maven-3.9.16-bin.zip")
	if err != nil {
		t.Fatal(err)
	}
	if hash != fakeSHA512 {
		t.Errorf("hash = %q, want %q", hash, fakeSHA512)
	}
}

func TestParseSHA512FileMalformedBareHash(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.sha512")
	os.WriteFile(f, []byte("not-a-hash\n"), 0644)

	if _, err := ParseSHA512File(f, "apache-maven-3.9.16-bin.zip"); err == nil {
		t.Error("expected error for malformed bare hash")
	}
}

func TestParseSHA512FileNameNotFound(t *testing.T) {
	content := fakeSHA512 + "  apache-maven-3.9.16-bin.zip\n"

	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha512")
	os.WriteFile(f, []byte(content), 0644)

	if _, err := ParseSHA512File(f, "nonexistent.zip"); err == nil {
		t.Error("expected error for missing filename")
	}
}
