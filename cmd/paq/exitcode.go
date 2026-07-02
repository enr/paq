package main

import "strings"

// Exit codes returned by the CLI. 0 (success) and 2 (panic) are handled
// elsewhere (Execute/main); the values below let scripts distinguish the
// most common failure classes without parsing error text.
const (
	exitError  = 1 // generic failure (network, missing config, ...)
	exitUsage  = 2 // wrong CLI usage: unknown flag/command, bad argument count
	exitVerify = 4 // checksum or signature verification failed
)

// exitCodeFor maps err to a process exit code. It relies on the error
// messages produced by cobra (usage errors) and by internal/verify /
// internal/install (verification failures), since neither currently exposes
// a typed/sentinel error for these cases.
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
// signature check, using the same message substrings hintFor already keys on.
func isVerifyError(err error) bool {
	msg := strings.ToLower(err.Error())
	verifySubstrings := []string{
		"sha256 mismatch",
		"sha512 mismatch",
		"integrity check",
		"signature verification failed",
		"minisign signature is invalid",
	}
	for _, s := range verifySubstrings {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
