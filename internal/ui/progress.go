package ui

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/enr/paq/internal/download"
)

// stderrIsTTY reports whether stderr, where progress is drawn, is an
// interactive terminal. A variable so tests can simulate either case.
var stderrIsTTY = func() bool { return term.IsTerminal(int(os.Stderr.Fd())) }

// NewProgressFn returns a ProgressFn that shows the download's progress on
// stderr: a text progress bar when colors are enabled, otherwise just the
// percentage. Without a terminal (CI logs, pipes) it returns nil: the
// "Downloading X..." and "Downloaded X" lines already frame the download,
// and \r-terminated updates would only fill the log.
func NewProgressFn(label string) download.ProgressFn {
	if Global.Quiet || !stderrIsTTY() {
		return nil
	}

	lastPct := -1
	lastMB := int64(0)

	return func(downloaded, total int64) {
		if total <= 0 {
			// Unknown size: print the bytes downloaded once per whole MB.
			// (Read chunks rarely land exactly on a MB boundary, so track the
			// last printed MB instead of testing for an exact multiple.)
			mb := downloaded / (1024 * 1024)
			if mb > lastMB {
				lastMB = mb
				fmt.Fprintf(os.Stderr, "\r%s: %d MB downloaded...", label, mb)
			}
			return
		}

		pct := int(float64(downloaded) / float64(total) * 100)
		if pct == lastPct {
			return
		}
		lastPct = pct

		if IsColorEnabled() {
			// ANSI text progress bar.
			width := 40
			filled := width * pct / 100
			bar := ""
			for i := 0; i < width; i++ {
				if i < filled {
					bar += "█"
				} else {
					bar += "░"
				}
			}
			fmt.Fprintf(os.Stderr, "\r%s [%s] %3d%%", label, bar, pct)
			if pct == 100 {
				fmt.Fprintln(os.Stderr)
			}
		} else {
			// Plain-text fallback with no special characters.
			fmt.Fprintf(os.Stderr, "\r%s: %d%%", label, pct)
			if pct == 100 {
				fmt.Fprintln(os.Stderr)
			}
		}
	}
}
