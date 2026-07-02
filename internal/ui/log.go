package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Styles for the different message types. The symbol and text share the same
// color so the outcome is readable at a glance: green = ok, red = error,
// yellow = warning.
var (
	stepStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))            // cyan
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true) // green
	failStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)  // red
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // yellow
	hintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))            // blue
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // gray
	debugStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // gray (body)
	// debugTagStyle colors the "[debug]" tag in bright magenta so the level
	// stands out immediately from the message's gray text.
	debugTagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	// Styles for the key/value rows of diagnostics (e.g. `doctor`): key in
	// neutral bold and aligned, value in cyan, so label and content are
	// distinguishable at a glance.
	fieldKeyStyle = lipgloss.NewStyle().Bold(true).Width(14)             // label
	fieldValStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // cyan (value)
)

// render formats "<symbol> <text>", coloring both when colors are enabled,
// otherwise returns the plain-text version.
func render(style lipgloss.Style, symbol, text string) string {
	if IsColorEnabled() {
		return style.Render(symbol + " " + text)
	}
	return symbol + " " + text
}

// Step prints an in-progress step message (e.g. "Downloading rg...").
// Suppressed with --quiet. In --json mode it is written to stderr to avoid
// polluting stdout with non-JSON text.
func Step(msg string, args ...any) {
	if Global.Quiet {
		return
	}
	out := os.Stdout
	if Global.JSON {
		out = os.Stderr
	}
	fmt.Fprintln(out, render(stepStyle, "→", fmt.Sprintf(msg, args...)))
}

// OK prints a success message (green).
// In --json mode it is written to stderr to avoid polluting stdout.
func OK(msg string, args ...any) {
	if Global.Quiet {
		return
	}
	out := os.Stdout
	if Global.JSON {
		out = os.Stderr
	}
	fmt.Fprintln(out, render(okStyle, "✓", fmt.Sprintf(msg, args...)))
}

// renderField formats a key/value row "<symbol> <key>: <value> <note>":
// the symbol is colored by the status (symStyle), the key in bold, the value
// in cyan and any note in gray. Without colors returns aligned plain text.
func renderField(symStyle lipgloss.Style, symbol, label, value, note string) string {
	if !IsColorEnabled() {
		line := fmt.Sprintf("%s %-14s %s", symbol, label+":", value)
		if note != "" {
			line += " " + note
		}
		return line
	}
	line := symStyle.Render(symbol) + " " + fieldKeyStyle.Render(label+":") + " " + fieldValStyle.Render(value)
	if note != "" {
		line += " " + dimStyle.Render(note)
	}
	return line
}

// OKField prints a diagnostic row "✓ key: value" (green).
// Like OK: on stdout, suppressed with --quiet, on stderr in --json mode.
func OKField(label, value string) {
	if Global.Quiet {
		return
	}
	out := os.Stdout
	if Global.JSON {
		out = os.Stderr
	}
	fmt.Fprintln(out, renderField(okStyle, "✓", label, value, ""))
}

// WarnField prints a diagnostic row "! key: value (note)" (yellow) to
// stderr; the note is in gray and can be empty. Like Warn, not gated by --quiet.
func WarnField(label, value, note string) {
	fmt.Fprintln(os.Stderr, renderField(warnStyle, "!", label, value, note))
}

// Fail prints an error message to stderr (red).
func Fail(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, render(failStyle, "✗", fmt.Sprintf(msg, args...)))
}

// Warn prints a visible warning to stderr (yellow).
// Unlike Info it is not gated by --verbose: it's used for security warnings
// (e.g. a tool installed without integrity verification) that must stay visible.
func Warn(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, render(warnStyle, "!", fmt.Sprintf(msg, args...)))
}

// Hint prints a suggestion to stderr (blue): guidance on how to resolve the
// error just shown. Suppressed with --quiet.
func Hint(msg string, args ...any) {
	if Global.Quiet {
		return
	}
	fmt.Fprintln(os.Stderr, render(hintStyle, "↪", "hint: "+fmt.Sprintf(msg, args...)))
}

// Info prints an informational message (with --verbose or --debug).
// In --json mode it is written to stderr to avoid polluting stdout.
func Info(msg string, args ...any) {
	if !Global.Verbose && !Global.Debug {
		return
	}
	out := os.Stdout
	if Global.JSON {
		out = os.Stderr
	}
	fmt.Fprintln(out, render(infoStyle, "·", fmt.Sprintf(msg, args...)))
}

// Debug prints detailed diagnostics to stderr (with --debug): shows internal
// steps (resolved URLs, temp paths, hashes, ...) to understand what paq is doing.
func Debug(msg string, args ...any) {
	if !Global.Debug {
		return
	}
	text := fmt.Sprintf(msg, args...)
	if IsColorEnabled() {
		fmt.Fprintln(os.Stderr, debugTagStyle.Render("[debug]")+" "+debugStyle.Render(text))
	} else {
		fmt.Fprintln(os.Stderr, "[debug] "+text)
	}
}
