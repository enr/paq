package download

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	data := []byte("archive")
	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:])

	if _, ok := FromCache(dir, key); ok {
		t.Fatal("FromCache hit on an empty cache")
	}

	src := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ToCache(dir, src, key); err != nil {
		t.Fatalf("ToCache: %v", err)
	}

	got, ok := FromCache(dir, key)
	if !ok {
		t.Fatal("FromCache missed a stored entry")
	}
	defer os.Remove(got)
	if b, _ := os.ReadFile(got); string(b) != string(data) {
		t.Errorf("cached content = %q, want %q", b, data)
	}
	// The caller removes the returned file: the cache entry must survive it.
	os.Remove(got)
	if _, err := os.Stat(filepath.Join(dir, key)); err != nil {
		t.Errorf("cache entry gone after removing the returned copy: %v", err)
	}
}

func TestFromCacheRejectsCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	key := hex.EncodeToString(make([]byte, 32)) // not the hash of the content below
	if err := os.WriteFile(filepath.Join(dir, key), []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := FromCache(dir, key); ok {
		t.Error("FromCache returned an entry whose content does not match its key")
	}
}

func TestCacheRejectsInvalidKey(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	os.WriteFile(src, []byte("x"), 0644)
	if err := ToCache(dir, src, "../escape"); err == nil {
		t.Error("ToCache accepted a key that is not a sha256 digest")
	}
	if _, ok := FromCache(dir, "../a"); ok {
		t.Error("FromCache accepted a key that is not a sha256 digest")
	}
}
