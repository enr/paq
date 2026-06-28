package verify

import "fmt"

// Plan descrive cosa verificare per un artefatto scaricato.
type Plan struct {
	// SHA256Literal è l'hash sha256 hardcoded nella spec (campo verify.sha256).
	SHA256Literal string
	// SHA256AssetPath è il path del file checksum scaricato (campo verify.sha256_asset).
	SHA256AssetPath string
	// SHA512Literal è l'hash sha512 hardcoded nella spec (campo verify.sha512).
	SHA512Literal string
	// SHA512AssetPath è il path del file checksum sha512 scaricato (campo verify.sha512_asset).
	SHA512AssetPath string
	// ArtifactName è il nome del file artefatto (usato per trovare la riga nel file checksum).
	ArtifactName string
	// MinisignPubKey è la chiave pubblica base64 per la verifica minisign.
	MinisignPubKey string
	// MinisignSigPath è il path del file di firma minisign scaricato.
	MinisignSigPath string
	// ArtifactPath è il path del file artefatto da verificare.
	ArtifactPath string
}

// Run esegue la verifica nell'ordine corretto:
// 1. Se configurata: verifica firma minisign del file checksum
// 2. Verifica sha256 dell'artefatto (da literal o dal file checksum)
func Run(plan Plan) error {
	// Passo 1: verifica firma minisign del file checksum (se configurata)
	// La firma deve essere verificata PRIMA di usare il checksum per verificare l'artefatto.
	if plan.MinisignPubKey != "" && plan.MinisignSigPath != "" {
		if plan.SHA256AssetPath == "" {
			return fmt.Errorf("minisign configured but no sha256_asset to sign")
		}
		if err := CheckMinisign(plan.SHA256AssetPath, plan.MinisignSigPath, plan.MinisignPubKey); err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}
	}

	// Passo 2: verifica sha256 dell'artefatto
	switch {
	case plan.SHA256Literal != "":
		if err := CheckFile(plan.ArtifactPath, plan.SHA256Literal); err != nil {
			return fmt.Errorf("integrity check failed: %w", err)
		}

	case plan.SHA256AssetPath != "":
		hash, err := ParseSHA256File(plan.SHA256AssetPath, plan.ArtifactName)
		if err != nil {
			return fmt.Errorf("read checksum: %w", err)
		}
		if err := CheckFile(plan.ArtifactPath, hash); err != nil {
			return fmt.Errorf("integrity check failed: %w", err)
		}
	}

	// Passo 3: verifica sha512 dell'artefatto
	switch {
	case plan.SHA512Literal != "":
		if err := CheckFileSHA512(plan.ArtifactPath, plan.SHA512Literal); err != nil {
			return fmt.Errorf("integrity check failed: %w", err)
		}

	case plan.SHA512AssetPath != "":
		hash, err := ParseSHA512File(plan.SHA512AssetPath, plan.ArtifactName)
		if err != nil {
			return fmt.Errorf("read checksum: %w", err)
		}
		if err := CheckFileSHA512(plan.ArtifactPath, hash); err != nil {
			return fmt.Errorf("integrity check failed: %w", err)
		}
	}

	return nil
}
