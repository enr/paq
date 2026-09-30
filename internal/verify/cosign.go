package verify

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// CheckCosignKey verifies that signaturePath is a valid key-based cosign
// signature ("cosign sign-blob --key") of filePath, produced by the private
// key matching pemPubKey (the PEM-encoded contents of cosign.pub).
//
// The signature file holds the base64-encoded signature. Supported keys are
// ECDSA P-256 (cosign's default: ASN.1 signature over the SHA-256 digest) and
// Ed25519 (signature over the raw file content).
func CheckCosignKey(filePath, signaturePath, pemPubKey string) error {
	pub, err := parseCosignPublicKey(pemPubKey)
	if err != nil {
		return fmt.Errorf("parse cosign public key: %w", err)
	}

	sigBytes, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("read signature file %s: %w", signaturePath, err)
	}

	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	// From here on the failures are verdicts on the signature itself, so they
	// carry ErrVerification (exit code 4), as in CheckMinisign.
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigBytes)))
	if err != nil {
		return failed("decode cosign signature: %v", err)
	}

	var valid bool
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		digest := sha256.Sum256(fileBytes)
		valid = ecdsa.VerifyASN1(k, digest[:], sig)
	case ed25519.PublicKey:
		valid = ed25519.Verify(k, fileBytes, sig)
	}
	if !valid {
		return failed("cosign signature is invalid for %s", filePath)
	}
	return nil
}

// parseCosignPublicKey decodes a PEM "PUBLIC KEY" block and accepts only the
// key types CheckCosignKey can verify.
func parseCosignPublicKey(pemPubKey string) (any, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemPubKey)))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() {
			return nil, fmt.Errorf("unsupported ECDSA curve %s (only P-256)", k.Curve.Params().Name)
		}
	case ed25519.PublicKey:
	default:
		return nil, fmt.Errorf("unsupported key type %T (only ECDSA P-256 and Ed25519)", pub)
	}
	return pub, nil
}
