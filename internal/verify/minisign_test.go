package verify

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	minisign "github.com/jedisct1/go-minisign"
)

// newTestMinisignKey generates an unencrypted Ed25519 minisign key pair,
// usable for signing in tests. Returns the private key and the base64-encoded
// public key (the same format expected by recipes).
func newTestMinisignKey(t *testing.T) (minisign.PrivateKey, string) {
	t.Helper()
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}

	var sk minisign.PrivateKey
	sk.SignatureAlgorithm = [2]byte{'E', 'd'}
	sk.KDFAlgorithm = [2]byte{0, 0} // unencrypted
	copy(sk.SecretKey[:], edPriv)
	if _, err := rand.Read(sk.KeyId[:]); err != nil {
		t.Fatalf("random key id: %v", err)
	}

	pk := sk.PublicKey()
	raw := make([]byte, 0, 42)
	raw = append(raw, pk.SignatureAlgorithm[:]...)
	raw = append(raw, pk.KeyId[:]...)
	raw = append(raw, pk.PublicKey[:]...)
	return sk, base64.StdEncoding.EncodeToString(raw)
}

// signToFile signs data with sk and writes the signature in minisign format
// to a temp file, returning its path.
func signToFile(t *testing.T, sk minisign.PrivateKey, data []byte) string {
	t.Helper()
	sig, err := sk.Sign(data, minisign.SignOptions{})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	encoded, err := sig.MarshalText()
	if err != nil {
		t.Fatalf("marshal signature: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sig.minisig")
	if err := os.WriteFile(path, encoded, 0644); err != nil {
		t.Fatalf("write signature: %v", err)
	}
	return path
}

func writeTempFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestCheckMinisignValid(t *testing.T) {
	sk, pub := newTestMinisignKey(t)
	data := []byte("payload to be signed")
	filePath := writeTempFile(t, "artifact.bin", data)
	sigPath := signToFile(t, sk, data)

	if err := CheckMinisign(filePath, sigPath, pub); err != nil {
		t.Errorf("expected valid signature, got error: %v", err)
	}
}

func TestCheckMinisignTamperedFile(t *testing.T) {
	sk, pub := newTestMinisignKey(t)
	sigPath := signToFile(t, sk, []byte("original content"))
	// File diverso da quello firmato.
	filePath := writeTempFile(t, "artifact.bin", []byte("tampered content"))

	if err := CheckMinisign(filePath, sigPath, pub); err == nil {
		t.Error("expected error for tampered file, got nil")
	}
}

func TestCheckMinisignWrongKey(t *testing.T) {
	signer, _ := newTestMinisignKey(t)
	_, otherPub := newTestMinisignKey(t) // non-matching public key
	data := []byte("payload")
	filePath := writeTempFile(t, "artifact.bin", data)
	sigPath := signToFile(t, signer, data)

	if err := CheckMinisign(filePath, sigPath, otherPub); err == nil {
		t.Error("expected error verifying with wrong public key, got nil")
	}
}

func TestCheckMinisignInvalidPubKey(t *testing.T) {
	sk, _ := newTestMinisignKey(t)
	data := []byte("payload")
	filePath := writeTempFile(t, "artifact.bin", data)
	sigPath := signToFile(t, sk, data)

	if err := CheckMinisign(filePath, sigPath, "not-a-valid-base64-key"); err == nil {
		t.Error("expected error for invalid public key, got nil")
	}
}
