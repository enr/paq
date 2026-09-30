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
	"fmt"
	"strings"
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
	filePath := writeTempFile(t, "checksums.txt", []byte("abc  tool.tar.gz\n"))
	sigPath := writeTempFile(t, "checksums.txt.sig", []byte("MEUCIQDl/uDLalNVjdQjhbIeaVn3RYubk6nDBPcgeK9EAx7sKQIgKwVuVPEPnRO8JEAX055Y5nuDT3Psn1xaiBmAwYYbmns="))
	if err := CheckCosignKey(filePath, sigPath, realCosignPub); err != nil {
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

// realCosignPub is the public key of the real cosign fixtures below.
const realCosignPub = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEqTRPXyZyWm2MFhyPWSaDLRUUphR/
liYwL4jbXuSfQOCVqzSavcPOA3t5/SEc/5dqmEw3ZxFX6D81iwVSfsmVrw==
-----END PUBLIC KEY-----
`

// realCosignBundle was produced by the real cosign binary (v3.1.3: "cosign
// sign-blob --key cosign.key --use-signing-config=false --tlog-upload=false
// --bundle checksums.txt.sigstore.json") over "abc  tool.tar.gz\n".
const realCosignBundle = `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{"publicKey":{"hint":"fDk+PLNDNKP7F+htxGzR0HoZ0Pa/Lmss1TS4hn8oDd8="}},"messageSignature":{"messageDigest":{"algorithm":"SHA2_256","digest":"iLztL7WfNjj0j1EYEnAfdoTR5iGEJCiLuqWIlEFZw44="},"signature":"MEUCIHHPJvjyK8MIuCQT0FhPmn35E3PqzF+LxNhL7GV8EbvxAiEAx3dOKGEywHKaC6p34roxxfxR4JNfxZgjPbxafFZALU4="}}`

func TestCheckCosignKeyBundleReal(t *testing.T) {
	filePath := writeTempFile(t, "checksums.txt", []byte("abc  tool.tar.gz\n"))
	bundlePath := writeTempFile(t, "checksums.txt.sigstore.json", []byte(realCosignBundle))
	if err := CheckCosignKeyBundle(filePath, bundlePath, realCosignPub); err != nil {
		t.Errorf("expected valid bundle, got error: %v", err)
	}
}

// cosignKeyBundle builds a bundle the way cosign does for a key-based blob
// signature, with the digest of digestOf and a signature over signed.
func cosignKeyBundle(t *testing.T, priv crypto.Signer, digestOf, signed []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(digestOf)
	sig := strings.TrimSpace(string(cosignSign(t, priv, signed)))
	return []byte(fmt.Sprintf(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","messageSignature":{"messageDigest":{"algorithm":"SHA2_256","digest":%q},"signature":%q}}`,
		base64.StdEncoding.EncodeToString(sum[:]), sig))
}

func TestCheckCosignKeyBundleFailures(t *testing.T) {
	priv := newECDSAKey(t)
	pub := pemPublicKey(t, priv.Public())
	data := []byte("checksums to be signed")

	cases := map[string]struct {
		file, bundle []byte
	}{
		"tampered file":   {file: []byte("tampered"), bundle: cosignKeyBundle(t, priv, data, data)},
		"different key":   {file: data, bundle: cosignKeyBundle(t, newECDSAKey(t), data, data)},
		"digest mismatch": {file: data, bundle: cosignKeyBundle(t, priv, []byte("other"), data)},
		"not JSON":        {file: data, bundle: []byte("not json")},
		"DSSE bundle":     {file: data, bundle: []byte(`{"dsseEnvelope":{"payload":"e30="}}`)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			filePath := writeTempFile(t, "checksums.txt", tc.file)
			bundlePath := writeTempFile(t, "checksums.txt.sigstore.json", tc.bundle)
			err := CheckCosignKeyBundle(filePath, bundlePath, pub)
			if !errors.Is(err, ErrVerification) {
				t.Errorf("error = %v, want ErrVerification", err)
			}
		})
	}
}
