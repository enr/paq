package main

import (
	"errors"
	"strings"

	"github.com/enr/paq/internal/verify"
)

// Exit codes returned by the CLI. 0 (success) and 2 (panic) are handled
// elsewhere (Execute/main); the values below let scripts distinguish the
// most common failure classes without parsing error text.
const (
	exitError  = 1 // generic failure (network, missing config, ...)
	exitUsage  = 2 // wrong CLI usage: unknown flag/command, bad argument count
	exitVerify = 4 // checksum or signature verification failed
)

// exitCodeFor maps err to a process exit code. Verification failures are
// recognized by verify.ErrVerification, which survives wrapping and any
// rewording of the message. Usage errors still rely on cobra's message
// prefixes, since cobra exposes nothing else to key on.
func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	switch {
	case isUsageError(err):
		return exitUsage
	case isVerifyError(err):
		return exitVerify
	default:
		return exitError
	}
}

// isUsageError reports whether err comes from cobra's own flag/argument
// parsing rather than from a command's RunE. Cobra does not tag these
// errors distinctly, so they are recognized by the fixed message prefixes
// it generates (see cobra's args.go and command.go).
func isUsageError(err error) bool {
	msg := err.Error()
	usagePrefixes := []string{
		"unknown command",
		"unknown flag:",
		"unknown shorthand flag:",
		"flag needs an argument:",
		"accepts ",
		"requires at least",
		"requires at most",
	}
	for _, p := range usagePrefixes {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// isVerifyError reports whether err originates from a failed integrity or
// signature check. The pipeline wraps these with %w all the way up (including
// through the aggregate error of a parallel install), so errors.Is finds them
// wherever they are nested.
//
// Note that an artifact paq could not read or a checksum document it could not
// parse are NOT verification failures: the check could not run, which is a
// generic error (exit 1), not "the file does not match" (exit 4).
func isVerifyError(err error) bool {
	return errors.Is(err, verify.ErrVerification)
}
