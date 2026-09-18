package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/enr/paq/internal/install"
	"github.com/enr/paq/internal/verify"
)

func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"generic", errors.New("network unreachable"), exitError},
		{"not found in manifest", errors.New(`app "rg" not found in manifest`), exitError},
		{"unknown flag", errors.New("unknown flag: --bogus"), exitUsage},
		{"unknown shorthand flag", errors.New(`unknown shorthand flag: 'x' in -x`), exitUsage},
		{"unknown command", errors.New(`unknown command "frobnicate" for "paq"`), exitUsage},
		{"flag needs an argument", errors.New("flag needs an argument: --dest"), exitUsage},
		{"too many args", errors.New("accepts at most 1 arg(s), received 2"), exitUsage},
		{"too few args", errors.New("requires at least 1 arg(s), only received 0"), exitUsage},
		// A message that merely reads like a verification failure is not one:
		// classification is by error identity, not by text.
		{"lookalike text", errors.New("sha256 mismatch for rg.tar.gz"), exitError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// artifact writes a file and returns its path.
func artifact(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The regression this guards: exit code 4 is the documented tamper signal, and
// it used to be derived by substring-matching the rendered message. These
// cases drive real failures through verify.Run, so rewording a message in
// internal/verify can no longer silently downgrade the exit code to 1.
func TestExitCodeForRealVerificationFailures(t *testing.T) {
	const wrongHash256 = "0000000000000000000000000000000000000000000000000000000000000000"
	const wrongHash512 = "00000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000"

	cases := []struct {
		name string
		plan verify.Plan
	}{
		{"sha256 literal mismatch", verify.Plan{
			ArtifactPath:  artifact(t, "real content"),
			SHA256Literal: wrongHash256,
		}},
		{"sha512 literal mismatch", verify.Plan{
			ArtifactPath:  artifact(t, "real content"),
			SHA512Literal: wrongHash512,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verify.Run(tc.plan)
			if err == nil {
				t.Fatal("expected verify.Run to fail")
			}
			if got := exitCodeFor(err); got != exitVerify {
				t.Errorf("exitCodeFor(%v) = %d, want %d", err, got, exitVerify)
			}
			// The classification must survive the wrapping the pipeline and
			// the commands add on the way up.
			wrapped := fmt.Errorf("install rg: %w", fmt.Errorf("verify: %w", err))
			if got := exitCodeFor(wrapped); got != exitVerify {
				t.Errorf("exitCodeFor(wrapped) = %d, want %d", got, exitVerify)
			}
		})
	}
}

// A check that could not be performed is a generic failure, not a tamper
// signal: exit 4 must mean "the file does not match", not "the file could not
// be read".
func TestExitCodeForUnperformableCheckIsGeneric(t *testing.T) {
	err := verify.Run(verify.Plan{
		ArtifactPath:  filepath.Join(t.TempDir(), "does-not-exist"),
		SHA256Literal: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected verify.Run to fail on a missing artifact")
	}
	if got := exitCodeFor(err); got != exitError {
		t.Errorf("exitCodeFor(%v) = %d, want %d", err, got, exitError)
	}
}

// A parallel run flattens its per-app errors into a human summary, which
// breaks the %w chain. batchError must still carry the verification verdict,
// or a batch install would exit 1 where a single install exits 4.
func TestExitCodeForBatchError(t *testing.T) {
	verifyFailed := batchError{msg: "0 installed, 1 failed: rg (sha256 mismatch)", failedVerification: true}
	if got := exitCodeFor(verifyFailed); got != exitVerify {
		t.Errorf("batch with a verification failure = %d, want %d", got, exitVerify)
	}

	otherFailed := batchError{msg: "0 installed, 1 failed: rg (network unreachable)"}
	if got := exitCodeFor(otherFailed); got != exitError {
		t.Errorf("batch without a verification failure = %d, want %d", got, exitError)
	}
}

// The batch summary is a new message that has not been printed yet, so it must
// not be mistaken for one the pipeline already showed — otherwise reportError
// swallows it and the user sees no summary at all.
func TestBatchErrorIsNotAlreadyShown(t *testing.T) {
	err := batchError{msg: "0 installed, 1 failed: rg (sha256 mismatch)", failedVerification: true}
	if install.ErrAlreadyShown(err) {
		t.Error("batchError must not report as already shown")
	}
}

func TestExitCodeForWrappedError(t *testing.T) {
	wrapped := hintError{msg: "install failed", hint: "retry"}
	if got := exitCodeFor(wrapped); got != exitError {
		t.Errorf("hintError should map to exitError by default, got %d", got)
	}
}
