package verify

import "fmt"

// Plan describes what to verify for a downloaded artifact.
type Plan struct {
	// SHA256Literal is the sha256 hash hardcoded in the spec (verify.sha256 field).
	SHA256Literal string
	// SHA256AssetPath is the path of the downloaded checksum file (verify.sha256_asset field).
	SHA256AssetPath string
	// SHA512Literal is the sha512 hash hardcoded in the spec (verify.sha512 field).
	SHA512Literal string
	// SHA512AssetPath is the path of the downloaded sha512 checksum file (verify.sha512_asset field).
	SHA512AssetPath string
	// ArtifactName is the artifact file name (used to find the line in the checksum file).
	ArtifactName string
	// MinisignPubKey is the base64 public key for minisign verification.
	MinisignPubKey string
	// MinisignSigPath is the path of the downloaded minisign signature file.
	MinisignSigPath string
	// ArtifactPath is the path of the artifact file to verify.
	ArtifactPath string
}

// Run performs verification in the correct order:
// 1. If configured: verify the checksum file's minisign signature.
// 2. Verify the artifact's sha256 (from a literal or from the checksum file).
func Run(plan Plan) error {
	// Step 1: verify the checksum file's minisign signature (if configured).
	// The signature must be verified BEFORE using the checksum to verify the artifact.
	if plan.MinisignPubKey != "" && plan.MinisignSigPath != "" {
		if plan.SHA256AssetPath == "" {
			return fmt.Errorf("minisign configured but no sha256_asset to sign")
		}
		if err := CheckMinisign(plan.SHA256AssetPath, plan.MinisignSigPath, plan.MinisignPubKey); err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}
	}

	// Step 2: verify the artifact's sha256.
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

	// Step 3: verify the artifact's sha512.
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
