package ui

import (
	"os"

	"golang.org/x/term"
)

// Config contains the global UX settings, set from flags/env at startup.
type Config struct {
	NoColor bool
	JSON    bool
	Quiet   bool
	Verbose bool
	Debug   bool
}

// Global is the current UX configuration, read by root.go before running commands.
var Global Config

// IsTTY returns true if stdout is connected to an interactive terminal.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// IsColorEnabled returns true if colors are enabled
// (TTY present, NO_COLOR not set, --no-color not specified).
func IsColorEnabled() bool {
	if Global.NoColor {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return IsTTY()
}
