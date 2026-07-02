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

func TestParseSHA512FileBareHash(t *testing.T) {
	// Apache Maven layout: the .sha512 file contains only the hash.
	const bare = "ed41650d42485cfc243fad22158caf9cbb5dc408ce7a09ddb94dd42a019de929"

	dir := t.TempDir()
	f := filepath.Join(dir, "apache-maven-3.9.16-bin.zip.sha512")
	os.WriteFile(f, []byte(bare+"\n"), 0644)

	hash, err := ParseSHA512File(f, "apache-maven-3.9.16-bin.zip")
	if err != nil {
		t.Fatal(err)
	}
	if hash != bare {
		t.Errorf("hash = %q, want %q", hash, bare)
	}
}

func TestParseSHA512FileWithFilename(t *testing.T) {
	content := "abc123  apache-maven-3.9.16-bin.zip\n" +
		"999zzz *other-file.zip\n"

	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha512")
	os.WriteFile(f, []byte(content), 0644)

	hash, err := ParseSHA512File(f, "apache-maven-3.9.16-bin.zip")
	if err != nil {
		t.Fatal(err)
	}
	if hash != "abc123" {
		t.Errorf("hash = %q, want abc123", hash)
	}
}
