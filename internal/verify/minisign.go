package verify

import (
	"fmt"
	"os"

	minisign "github.com/jedisct1/go-minisign"
)

// CheckMinisign verifica che signaturePath sia una firma minisign valida
// di filePath, prodotta dalla chiave pubblica pubKeyBase64.
func CheckMinisign(filePath, signaturePath, pubKeyBase64 string) error {
	pk, err := minisign.NewPublicKey(pubKeyBase64)
	if err != nil {
		return fmt.Errorf("parse minisign public key: %w", err)
	}

	sigBytes, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("read signature file %s: %w", signaturePath, err)
	}

	sig, err := minisign.DecodeSignature(string(sigBytes))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	valid, err := pk.Verify(fileBytes, sig)
	if err != nil {
		return fmt.Errorf("verify minisign signature: %w", err)
	}
	if !valid {
		return fmt.Errorf("minisign signature is invalid for %s", filePath)
	}
	return nil
}
