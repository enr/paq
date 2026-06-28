package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckFile(t *testing.T) {
	content := []byte("hello paq")
	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])

	tmp, _ := os.CreateTemp(t.TempDir(), "check-*")
	tmp.Write(content)
	tmp.Close()

	// hash corretto
	if err := CheckFile(tmp.Name(), expected); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// hash sbagliato
	if err := CheckFile(tmp.Name(), "deadbeef"); err == nil {
		t.Error("expected error for wrong hash, got nil")
	}
}

func TestParseSHA256File(t *testing.T) {
	content := "abc123def456  ripgrep-14.1.1-x86_64-unknown-linux-gnu.tar.gz\n" +
		"111aaa222bbb *ripgrep-14.1.1-x86_64-unknown-linux-gnu.tar.gz.sig\n"

	dir := t.TempDir()
	f := filepath.Join(dir, "checksums.sha256")
	os.WriteFile(f, []byte(content), 0644)

	hash, err := ParseSHA256File(f, "ripgrep-14.1.1-x86_64-unknown-linux-gnu.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if hash != "abc123def456" {
		t.Errorf("hash = %q, want abc123def456", hash)
	}

	_, err = ParseSHA256File(f, "nonexistent.tar.gz")
	if err == nil {
		t.Error("expected error for missing filename")
	}
}
