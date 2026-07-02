package ui

import (
	"fmt"
	"os"

	"github.com/enr/paq/internal/download"
)

// NewProgressFn returns a ProgressFn that shows the download's progress.
// If the terminal is a TTY and colors are enabled, uses a text progress bar on stderr.
// Otherwise, prints just the percentage.
func NewProgressFn(label string) download.ProgressFn {
	if Global.Quiet {
		return nil
	}

	lastPct := -1

	return func(downloaded, total int64) {
		if total <= 0 {
			// Unknown size: print only the bytes downloaded every MB.
			mb := downloaded / (1024 * 1024)
			if downloaded%(1024*1024) == 0 && mb > 0 {
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
