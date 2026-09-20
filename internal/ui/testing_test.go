package ui

import (
	"io"
	"os"
	"testing"
)

// captureOutput redirects os.Stdout and os.Stderr for the duration of fn and
// returns everything written to each.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	fn()

	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = origOut, origErr

	outBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	errBytes, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	return string(outBytes), string(errBytes)
}

// withGlobal runs fn with Global set to cfg, restoring the previous value afterwards.
func withGlobal(t *testing.T, cfg Config, fn func()) (stdout, stderr string) {
	t.Helper()
	saved := Global
	Global = cfg
	defer func() { Global = saved }()
	return captureOutput(t, fn)
}
