package main

import (
	"errors"
	"testing"
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
		{"sha256 mismatch", errors.New("sha256 mismatch for rg.tar.gz:\n  got:  a\n  want: b"), exitVerify},
		{"sha512 mismatch", errors.New("sha512 mismatch for x"), exitVerify},
		{"integrity check", errors.New("integrity check failed: sha256 mismatch"), exitVerify},
		{"signature verification failed", errors.New("signature verification failed: minisign signature is invalid for x"), exitVerify},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestExitCodeForWrappedError(t *testing.T) {
	inner := errors.New("sha256 mismatch for x")
	wrapped := hintError{msg: "install failed", hint: "retry"}
	if got := exitCodeFor(wrapped); got != exitError {
		t.Errorf("hintError should map to exitError by default, got %d", got)
	}
	if got := exitCodeFor(inner); got != exitVerify {
		t.Errorf("exitCodeFor(inner) = %d, want exitVerify", got)
	}
}
