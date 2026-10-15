package verify

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
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

// TestRunNoVerification documents that an empty plan performs no verification
// and returns no error: this is exactly the case that justifies the warning
// emitted by the pipeline when a spec configures no verification.
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
	// Oracle JDK layout: the .sha256 file contains only the hash.
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
	// Apache Maven layout: the .sha512 file contains only the hash.
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

// TestRunSignatureThenChecksum verifies the full chain: minisign signature on
// the checksum file, then sha256 integrity of the artifact against that checksum.
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

// TestRunSignatureCheckedBeforeChecksum verifies the order: if the checksum
// file's signature is invalid, Run must fail at the signature step even if
// the checksum would match the artifact.
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
		MinisignPubKey:  wrongPub, // signature not verifiable
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
		// SHA256AssetPath absent: the signature has no checksum to sign.
	}
	err := Run(plan)
	if err == nil {
		t.Fatal("expected error when minisign configured without checksum asset")
	}
	if !strings.Contains(err.Error(), "no sha256_asset") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ErrVerification is the contract cmd/paq maps to exit code 4. These tests pin
// which failures carry it: a verdict ("the file does not match") does, an
// error that merely prevented the check from running does not.
func TestErrVerificationTagsMismatches(t *testing.T) {
	artifact := writeTempFile(t, "artifact.bin", []byte("real content"))
	const wrong256 = "0000000000000000000000000000000000000000000000000000000000000000"
	const wrong512 = wrong256 + wrong256

	t.Run("sha256 mismatch", func(t *testing.T) {
		err := CheckFile(artifact, wrong256)
		if !errors.Is(err, ErrVerification) {
			t.Errorf("err = %v, want it to satisfy errors.Is(_, ErrVerification)", err)
		}
	})

	t.Run("sha512 mismatch", func(t *testing.T) {
		err := CheckFileSHA512(artifact, wrong512)
		if !errors.Is(err, ErrVerification) {
			t.Errorf("err = %v, want it to satisfy errors.Is(_, ErrVerification)", err)
		}
	})

	t.Run("invalid minisign signature", func(t *testing.T) {
		signer, _ := newTestMinisignKey(t)
		_, otherPub := newTestMinisignKey(t)
		data := []byte("payload")
		signed := writeTempFile(t, "signed.bin", data)
		sigPath := signToFile(t, signer, data)

		err := CheckMinisign(signed, sigPath, otherPub)
		if !errors.Is(err, ErrVerification) {
			t.Errorf("err = %v, want it to satisfy errors.Is(_, ErrVerification)", err)
		}
	})

	// The tag must survive the wrapping Run and the pipeline add.
	t.Run("survives wrapping", func(t *testing.T) {
		err := Run(Plan{ArtifactPath: artifact, SHA256Literal: wrong256})
		if !errors.Is(err, ErrVerification) {
			t.Errorf("Run err = %v, want it to satisfy errors.Is(_, ErrVerification)", err)
		}
	})
}

func TestErrVerificationDoesNotTagUnperformableChecks(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.bin")
	const wrong256 = "0000000000000000000000000000000000000000000000000000000000000000"

	t.Run("unreadable artifact", func(t *testing.T) {
		err := CheckFile(missing, wrong256)
		if err == nil {
			t.Fatal("expected an error")
		}
		if errors.Is(err, ErrVerification) {
			t.Errorf("err = %v, must NOT be tagged: the check could not run", err)
		}
	})

	t.Run("unreadable checksum document", func(t *testing.T) {
		err := Run(Plan{
			ArtifactPath:    writeTempFile(t, "a.bin", []byte("x")),
			SHA256AssetPath: missing,
			ArtifactName:    "a.bin",
		})
		if err == nil {
			t.Fatal("expected an error")
		}
		if errors.Is(err, ErrVerification) {
			t.Errorf("err = %v, must NOT be tagged: the check could not run", err)
		}
	})
}

// TestPlanExpectedSHA256 covers each source of the expected hash, without an
// artifact: the pipeline asks for it before the download.
func TestPlanExpectedSHA256(t *testing.T) {
	sum := sha256Hex([]byte("artifact"))
	upper := strings.ToUpper(sum)
	coreutils := writeTempFile(t, "SHA256SUMS", []byte(upper+"  artifact.tar.gz\n"+sha256Hex([]byte("other"))+"  other.tar.gz\n"))
	jsonDoc := writeTempFile(t, "release.json", []byte(`{"files":{"linux":{"sha256":"`+sum+`"}}}`))

	cases := []struct {
		name string
		plan Plan
		want string
	}{
		{"none", Plan{ArtifactName: "artifact.tar.gz"}, ""},
		{"literal, normalized", Plan{SHA256Literal: " " + upper + " "}, sum},
		{"checksum file, by name", Plan{SHA256AssetPath: coreutils, ArtifactName: "artifact.tar.gz"}, sum},
		{"checksum json, by selector", Plan{SHA256AssetPath: jsonDoc, SHA256Selector: "files.linux.sha256", ArtifactName: "artifact.tar.gz"}, sum},
		{"literal wins over document", Plan{SHA256Literal: sum, SHA256AssetPath: filepath.Join(t.TempDir(), "missing")}, sum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.plan.ExpectedSHA256()
			if err != nil {
				t.Fatalf("ExpectedSHA256: %v", err)
			}
			if got != tc.want {
				t.Errorf("ExpectedSHA256() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlanExpectedSHA256UnreadableDocument(t *testing.T) {
	doc := writeTempFile(t, "SHA256SUMS", []byte(sha256Hex([]byte("other"))+"  other.tar.gz\n"))
	_, err := Plan{SHA256AssetPath: doc, ArtifactName: "artifact.tar.gz"}.ExpectedSHA256()
	if err == nil || !strings.HasPrefix(err.Error(), "read checksum:") {
		t.Errorf("error = %v, want a read checksum error", err)
	}
	if errors.Is(err, ErrVerification) {
		t.Error("a checksum document without the artifact is an unperformable check, not a mismatch")
	}
}
