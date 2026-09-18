package verify

import (
	"fmt"
	"os"

	minisign "github.com/jedisct1/go-minisign"
)

// CheckMinisign verifies that signaturePath is a valid minisign signature of
// filePath, produced by the pubKeyBase64 public key.
func CheckMinisign(filePath, signaturePath, pubKeyBase64 string) error {
	pk, err := minisign.NewPublicKey(pubKeyBase64)
	if err != nil {
		return fmt.Errorf("parse minisign public key: %w", err)
	}

	sigBytes, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("read signature file %s: %w", signaturePath, err)
	}

	// From here on the failures are verdicts on the signature itself, so they
	// carry ErrVerification (exit code 4): a malformed .minisig is a signature
	// that does not check out, and Verify reports a signature made by another
	// key as an error ("incompatible key identifiers") rather than as
	// valid == false. Only the public key and the two file reads above are
	// "could not check" cases.
	sig, err := minisign.DecodeSignature(string(sigBytes))
	if err != nil {
		return failed("decode signature: %v", err)
	}

	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	valid, err := pk.Verify(fileBytes, sig)
	if err != nil {
		return failed("verify minisign signature: %v", err)
	}
	if !valid {
		return failed("minisign signature is invalid for %s", filePath)
	}
	return nil
}
