package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// CosignIdentity is the certificate policy a keyless signature must satisfy:
// the OIDC issuer and either the exact signer identity (certificate SAN) or
// an anchored regexp over it.
type CosignIdentity struct {
	Issuer         string
	Identity       string
	IdentityRegexp string
}

// CheckCosignBundle verifies a keyless Sigstore bundle for filePath by running
// "cosign verify-blob" (cosignPath) against the identity policy.
//
// cosign exits 1 both for an invalid signature and for a check it could not
// complete (e.g. no network for its trust root), and only its message tells
// them apart. Every non-zero exit is therefore treated as a failed
// verification (fail closed), with cosign's stderr as the reason; only a
// binary that cannot be started at all is a "could not check" error.
func CheckCosignBundle(ctx context.Context, cosignPath, filePath, bundlePath string, id CosignIdentity) error {
	args := []string{"verify-blob", "--bundle", bundlePath, "--certificate-oidc-issuer", id.Issuer}
	if id.IdentityRegexp != "" {
		args = append(args, "--certificate-identity-regexp", id.IdentityRegexp)
	} else {
		args = append(args, "--certificate-identity", id.Identity)
	}
	args = append(args, filePath)
	return runCosign(ctx, cosignPath, args)
}

// runCosign runs cosign with args. A non-zero exit is a failed verification
// (see CheckCosignBundle); only a binary that cannot be started is not.
func runCosign(ctx context.Context, cosignPath string, args []string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, cosignPath, args...)
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// cosign repeats its error ("Error: ..." then "error during command
		// execution: ..."): the first line is enough.
		msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return failed("cosign: %s", msg)
	}
	if err != nil {
		return fmt.Errorf("run %s: %w", cosignPath, err)
	}
	return nil
}

// GitHubActionsIssuer is the OIDC issuer of GitHub Actions workflows.
const GitHubActionsIssuer = "https://token.actions.githubusercontent.com"

// AttestationPolicy says who must have produced a GitHub build-provenance
// attestation: a workflow run of Repo ("owner/name"), optionally by one
// SignerWorkflow ("owner/name/.github/workflows/file.yml").
type AttestationPolicy struct {
	Repo           string
	SignerWorkflow string
}

// CheckGitHubAttestations verifies that at least one of bundles (Sigstore
// bundles of GitHub attestations) is a valid SLSA provenance attestation for
// artifactPath satisfying policy, by running "cosign
// verify-blob-attestation". No bundle at all is a failed verification.
func CheckGitHubAttestations(ctx context.Context, cosignPath, artifactPath string, bundles [][]byte, policy AttestationPolicy) error {
	if len(bundles) == 0 {
		return failed("no GitHub attestation found for %s in %s", artifactPath, policy.Repo)
	}
	// Like "gh attestation verify": the certificate must come from a
	// workflow of the repository (or the given signer workflow), and the run
	// must have been triggered in the repository itself.
	identity := "^" + regexp.QuoteMeta("https://github.com/"+policy.Repo+"/")
	if policy.SignerWorkflow != "" {
		identity = "^" + regexp.QuoteMeta("https://github.com/"+policy.SignerWorkflow+"@")
	}

	var lastErr error
	for _, b := range bundles {
		err := checkAttestationBundle(ctx, cosignPath, artifactPath, b, []string{
			"verify-blob-attestation",
			"--type", "https://slsa.dev/provenance/v1",
			"--certificate-oidc-issuer", GitHubActionsIssuer,
			"--certificate-identity-regexp", identity,
			"--certificate-github-workflow-repository", policy.Repo,
		})
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// checkAttestationBundle writes bundle to a temp file and runs cosign with
// args plus "--bundle <file> <artifact>".
func checkAttestationBundle(ctx context.Context, cosignPath, artifactPath string, bundle []byte, args []string) error {
	f, err := os.CreateTemp("", "paq-attestation-*.json")
	if err != nil {
		return fmt.Errorf("create bundle file: %w", err)
	}
	defer os.Remove(f.Name())
	_, err = f.Write(bundle)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write bundle file: %w", err)
	}
	return runCosign(ctx, cosignPath, append(args, "--bundle", f.Name(), artifactPath))
}
