package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
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
