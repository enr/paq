package verify

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"testing"
)

// pemPublicKey encodes pub in the PKIX PEM form of cosign.pub.
func pemPublicKey(t *testing.T, pub crypto.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// cosignSign signs data the way "cosign sign-blob --key" does and returns the
// base64-encoded signature (the content of the .sig file).
func cosignSign(t *testing.T, priv crypto.Signer, data []byte) []byte {
	t.Helper()
	var sig []byte
	var err error
	switch k := priv.(type) {
	case *ecdsa.PrivateKey:
		digest := sha256.Sum256(data)
		sig, err = ecdsa.SignASN1(rand.Reader, k, digest[:])
	case ed25519.PrivateKey:
		sig = ed25519.Sign(k, data)
	default:
		t.Fatalf("unsupported key %T", priv)
	}
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return []byte(base64.StdEncoding.EncodeToString(sig) + "\n")
}

func newECDSAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	return k
}

func newEd25519Key(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	return k
}

func TestCheckCosignKeyValid(t *testing.T) {
	data := []byte("checksums to be signed")
	for name, priv := range map[string]crypto.Signer{
		"ecdsa":   newECDSAKey(t),
		"ed25519": newEd25519Key(t),
	} {
		t.Run(name, func(t *testing.T) {
			filePath := writeTempFile(t, "checksums.txt", data)
			sigPath := writeTempFile(t, "checksums.txt.sig", cosignSign(t, priv, data))
			if err := CheckCosignKey(filePath, sigPath, pemPublicKey(t, priv.Public())); err != nil {
				t.Errorf("expected valid signature, got error: %v", err)
			}
		})
	}
}

// TestCheckCosignKeyRealSignature checks a signature produced by the real
// cosign binary (v3.1.3: "cosign sign-blob --key cosign.key
// --new-bundle-format=false --use-signing-config=false --tlog-upload=false"),
// so the format assumptions above are pinned to what cosign actually emits.
func TestCheckCosignKeyRealSignature(t *testing.T) {
	const pub = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEqTRPXyZyWm2MFhyPWSaDLRUUphR/
liYwL4jbXuSfQOCVqzSavcPOA3t5/SEc/5dqmEw3ZxFX6D81iwVSfsmVrw==
-----END PUBLIC KEY-----
`
	filePath := writeTempFile(t, "checksums.txt", []byte("abc  tool.tar.gz\n"))
	sigPath := writeTempFile(t, "checksums.txt.sig", []byte("MEUCIQDl/uDLalNVjdQjhbIeaVn3RYubk6nDBPcgeK9EAx7sKQIgKwVuVPEPnRO8JEAX055Y5nuDT3Psn1xaiBmAwYYbmns="))
	if err := CheckCosignKey(filePath, sigPath, pub); err != nil {
		t.Errorf("expected valid signature, got error: %v", err)
	}
}

func TestCheckCosignKeyFailures(t *testing.T) {
	priv := newECDSAKey(t)
	pub := pemPublicKey(t, priv.Public())
	data := []byte("checksums to be signed")

	cases := map[string]struct {
		file, sig []byte
		pub       string
	}{
		"tampered file":       {file: []byte("tampered"), sig: cosignSign(t, priv, data), pub: pub},
		"different key":       {file: data, sig: cosignSign(t, newECDSAKey(t), data), pub: pub},
		"malformed signature": {file: data, sig: []byte("not base64!!"), pub: pub},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			filePath := writeTempFile(t, "checksums.txt", tc.file)
			sigPath := writeTempFile(t, "checksums.txt.sig", tc.sig)
			err := CheckCosignKey(filePath, sigPath, tc.pub)
			if !errors.Is(err, ErrVerification) {
				t.Errorf("error = %v, want ErrVerification", err)
			}
		})
	}
}

// TestCheckCosignKeyBadPublicKey verifies that an unusable public key is a
// "could not check" error, not a verification verdict.
func TestCheckCosignKeyBadPublicKey(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate p384 key: %v", err)
	}
	data := []byte("checksums")
	filePath := writeTempFile(t, "checksums.txt", data)
	sigPath := writeTempFile(t, "checksums.txt.sig", cosignSign(t, newECDSAKey(t), data))

	for name, pub := range map[string]string{
		"not PEM":   "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
		"RSA key":   pemPublicKey(t, rsaKey.Public()),
		"P-384 key": pemPublicKey(t, p384.Public()),
	} {
		t.Run(name, func(t *testing.T) {
			err := CheckCosignKey(filePath, sigPath, pub)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if errors.Is(err, ErrVerification) {
				t.Errorf("error = %v, must not be ErrVerification", err)
			}
		})
	}
}
