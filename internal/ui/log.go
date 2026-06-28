package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Stili per i diversi tipi di messaggio. Il simbolo e il testo condividono il
// colore così che l'esito sia leggibile a colpo d'occhio: verde = ok, rosso =
// errore, giallo = avviso.
var (
	stepStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))            // ciano
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true) // verde
	failStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)  // rosso
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // giallo
	hintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))            // blu
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // grigio
	debugStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // grigio (corpo)
	// debugTagStyle colora il tag "[debug]" in magenta acceso così il livello
	// si distingue subito dal testo grigio del messaggio.
	debugTagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
)

// render formatta "<symbol> <text>" colorando entrambi quando i colori sono
// abilitati, altrimenti ritorna la versione in solo testo.
func render(style lipgloss.Style, symbol, text string) string {
	if IsColorEnabled() {
		return style.Render(symbol + " " + text)
	}
	return symbol + " " + text
}

// Step stampa un messaggio di step in corso (es. "Downloading rg...").
// Soppresso con --quiet.
func Step(msg string, args ...any) {
	if Global.Quiet {
		return
	}
	fmt.Println(render(stepStyle, "→", fmt.Sprintf(msg, args...)))
}

// OK stampa un messaggio di successo (verde).
func OK(msg string, args ...any) {
	if Global.Quiet {
		return
	}
	fmt.Println(render(okStyle, "✓", fmt.Sprintf(msg, args...)))
}

// Fail stampa un messaggio di errore su stderr (rosso).
func Fail(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, render(failStyle, "✗", fmt.Sprintf(msg, args...)))
}

// Warn stampa un avviso visibile su stderr (giallo).
// A differenza di Info non è gated da --verbose: è usato per avvisi di sicurezza
// (es. tool installato senza verifica di integrità) che devono restare visibili.
func Warn(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, render(warnStyle, "!", fmt.Sprintf(msg, args...)))
}

// Hint stampa un suggerimento su stderr (blu): un'indicazione su come risolvere
// l'errore appena mostrato. Soppresso con --quiet.
func Hint(msg string, args ...any) {
	if Global.Quiet {
		return
	}
	fmt.Fprintln(os.Stderr, render(hintStyle, "↪", "hint: "+fmt.Sprintf(msg, args...)))
}

// Info stampa un messaggio informativo (con --verbose o --debug).
func Info(msg string, args ...any) {
	if !Global.Verbose && !Global.Debug {
		return
	}
	fmt.Println(render(infoStyle, "·", fmt.Sprintf(msg, args...)))
}

// Debug stampa diagnostica dettagliata su stderr (con --debug): mostra i
// passaggi interni (URL risolti, path temporanei, hash, ...) per capire cosa
// sta facendo paq.
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
