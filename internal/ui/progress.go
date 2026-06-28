package ui

import (
	"fmt"
	"os"

	"github.com/enr/paq/internal/download"
)

// NewProgressFn ritorna una ProgressFn che mostra il progresso del download.
// Se il terminale è un TTY e i colori sono abilitati, usa una barra testuale su stderr.
// Altrimenti, stampa solo la percentuale.
func NewProgressFn(label string) download.ProgressFn {
	if Global.Quiet {
		return nil
	}

	lastPct := -1

	return func(downloaded, total int64) {
		if total <= 0 {
			// Dimensione ignota: stampa solo i byte scaricati ogni MB
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
			// Barra di progresso testuale ANSI
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
			// Fallback testuale senza caratteri speciali
			fmt.Fprintf(os.Stderr, "\r%s: %d%%", label, pct)
			if pct == 100 {
				fmt.Fprintln(os.Stderr)
			}
		}
	}
}
