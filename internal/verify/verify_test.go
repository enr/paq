package verify

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func sha512Hex(b []byte) string {
	s := sha512.Sum512(b)
	return hex.EncodeToString(s[:])
}

// TestRunNoVerification documenta che un piano vuoto non esegue alcuna verifica
// e non ritorna errore: è esattamente il caso che giustifica il warning emesso
// dalla pipeline quando una spec non configura la verifica.
func TestRunNoVerification(t *testing.T) {
	artifact := writeTempFile(t, "artifact.bin", []byte("anything"))
	if err := Run(Plan{ArtifactPath: artifact, ArtifactName: "artifact.bin"}); err != nil {
		t.Errorf("empty plan should not error, got: %v", err)
	}
}

func TestRunSHA256Literal(t *testing.T) {
	data := []byte("integrity-256")
	artifact := writeTempFile(t, "artifact.bin", data)

	if err := Run(Plan{ArtifactPath: artifact, SHA256Literal: sha256Hex(data)}); err != nil {
		t.Errorf("valid sha256 literal failed: %v", err)
	}

	if err := Run(Plan{ArtifactPath: artifact, SHA256Literal: "deadbeef"}); err == nil {
		t.Error("expected error for wrong sha256 literal")
	}
}

func TestRunSHA512Literal(t *testing.T) {
	data := []byte("integrity-512")
	artifact := writeTempFile(t, "artifact.bin", data)

	if err := Run(Plan{ArtifactPath: artifact, SHA512Literal: sha512Hex(data)}); err != nil {
		t.Errorf("valid sha512 literal failed: %v", err)
	}

	if err := Run(Plan{ArtifactPath: artifact, SHA512Literal: "deadbeef"}); err == nil {
		t.Error("expected error for wrong sha512 literal")
	}
}

func TestRunSHA256Asset(t *testing.T) {
	data := []byte("checksum-asset-256")
	artifact := writeTempFile(t, "artifact.bin", data)
	checksum := writeTempFile(t, "artifact.bin.sha256",
		[]byte(fmt.Sprintf("%s  artifact.bin\n", sha256Hex(data))))

	plan := Plan{ArtifactPath: artifact, ArtifactName: "artifact.bin", SHA256AssetPath: checksum}
	if err := Run(plan); err != nil {
		t.Errorf("valid sha256 asset failed: %v", err)
	}

	bad := writeTempFile(t, "bad.sha256", []byte("deadbeef  artifact.bin\n"))
	plan.SHA256AssetPath = bad
	if err := Run(plan); err == nil {
		t.Error("expected error for mismatching sha256 asset")
	}
}

func TestRunSHA256AssetBareHash(t *testing.T) {
	// Layout Oracle JDK: il file .sha256 contiene solo l'hash.
	data := []byte("checksum-asset-256-bare")
	artifact := writeTempFile(t, "jdk-26_linux-aarch64_bin.tar.gz", data)
	checksum := writeTempFile(t, "jdk-26_linux-aarch64_bin.tar.gz.sha256",
		[]byte(sha256Hex(data)+"\n"))

	plan := Plan{
		ArtifactPath:    artifact,
		ArtifactName:    "jdk-26_linux-aarch64_bin.tar.gz",
		SHA256AssetPath: checksum,
	}
	if err := Run(plan); err != nil {
		t.Errorf("valid sha256 bare-hash asset failed: %v", err)
	}

	bad := writeTempFile(t, "bad-bare.sha256", []byte(strings.Repeat("0", 64)+"\n"))
	plan.SHA256AssetPath = bad
	if err := Run(plan); err == nil {
		t.Error("expected error for mismatching sha256 asset")
	}
}

func TestRunSHA512AssetBareHash(t *testing.T) {
	// Layout Apache Maven: il file .sha512 contiene solo l'hash.
	data := []byte("checksum-asset-512")
	artifact := writeTempFile(t, "apache-maven-x-bin.zip", data)
	checksum := writeTempFile(t, "apache-maven-x-bin.zip.sha512",
		[]byte(sha512Hex(data)+"\n"))

	plan := Plan{
		ArtifactPath:    artifact,
		ArtifactName:    "apache-maven-x-bin.zip",
		SHA512AssetPath: checksum,
	}
	if err := Run(plan); err != nil {
		t.Errorf("valid sha512 bare-hash asset failed: %v", err)
	}

	bad := writeTempFile(t, "bad.sha512", []byte(strings.Repeat("0", 128)+"\n"))
	plan.SHA512AssetPath = bad
	if err := Run(plan); err == nil {
		t.Error("expected error for mismatching sha512 asset")
	}
}

// TestRunSignatureThenChecksum verifica la catena completa: firma minisign sul
// file checksum, poi integrità sha256 dell'artefatto contro quel checksum.
func TestRunSignatureThenChecksum(t *testing.T) {
	sk, pub := newTestMinisignKey(t)
	data := []byte("the-real-artifact")
	artifact := writeTempFile(t, "artifact.bin", data)

	checksumContent := []byte(fmt.Sprintf("%s  artifact.bin\n", sha256Hex(data)))
	checksum := writeTempFile(t, "artifact.bin.sha256", checksumContent)
	sigPath := signToFile(t, sk, checksumContent)

	plan := Plan{
		ArtifactPath:    artifact,
		ArtifactName:    "artifact.bin",
		SHA256AssetPath: checksum,
		MinisignPubKey:  pub,
		MinisignSigPath: sigPath,
	}
	if err := Run(plan); err != nil {
		t.Errorf("full signature+checksum chain failed: %v", err)
	}
}

// TestRunSignatureCheckedBeforeChecksum verifica l'ordine: se la firma del file
// checksum è invalida, Run deve fallire alla firma anche se il checksum
// combacerebbe con l'artefatto.
func TestRunSignatureCheckedBeforeChecksum(t *testing.T) {
	signer, _ := newTestMinisignKey(t)
	_, wrongPub := newTestMinisignKey(t)
	data := []byte("artifact-data")
	artifact := writeTempFile(t, "artifact.bin", data)

	checksumContent := []byte(fmt.Sprintf("%s  artifact.bin\n", sha256Hex(data)))
	checksum := writeTempFile(t, "artifact.bin.sha256", checksumContent)
	sigPath := signToFile(t, signer, checksumContent)

	plan := Plan{
		ArtifactPath:    artifact,
		ArtifactName:    "artifact.bin",
		SHA256AssetPath: checksum,
		MinisignPubKey:  wrongPub, // firma non verificabile
		MinisignSigPath: sigPath,
	}
	err := Run(plan)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("expected signature failure, got: %v", err)
	}
}

func TestRunMinisignWithoutChecksumAsset(t *testing.T) {
	sk, pub := newTestMinisignKey(t)
	data := []byte("artifact")
	artifact := writeTempFile(t, "artifact.bin", data)
	sigPath := signToFile(t, sk, data)

	plan := Plan{
		ArtifactPath:    artifact,
		ArtifactName:    "artifact.bin",
		MinisignPubKey:  pub,
		MinisignSigPath: sigPath,
		// SHA256AssetPath assente: la firma non ha un checksum da firmare.
	}
	err := Run(plan)
	if err == nil {
		t.Fatal("expected error when minisign configured without checksum asset")
	}
	if !strings.Contains(err.Error(), "no sha256_asset") {
		t.Errorf("unexpected error: %v", err)
	}
}
