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
	// Stili per le righe chiave/valore della diagnostica (es. `doctor`): chiave
	// in grassetto neutro e allineata, valore in ciano, così da distinguere
	// a colpo d'occhio etichetta e contenuto.
	fieldKeyStyle = lipgloss.NewStyle().Bold(true).Width(14)             // etichetta
	fieldValStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // ciano (valore)
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
// Soppresso con --quiet. In modalità --json viene scritto su stderr per non
// sporcare stdout con testo non-JSON.
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

// OK stampa un messaggio di successo (verde).
// In modalità --json viene scritto su stderr per non sporcare stdout.
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

// renderField formatta una riga chiave/valore "<symbol> <key>: <value> <note>":
// il simbolo è colorato dallo stato (symStyle), la chiave in grassetto, il valore
// in ciano e l'eventuale nota in grigio. Senza colori ritorna testo allineato.
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

// OKField stampa una riga diagnostica "✓ chiave: valore" (verde).
// Come OK: su stdout, soppresso con --quiet, su stderr in modalità --json.
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

// WarnField stampa una riga diagnostica "! chiave: valore (nota)" (giallo) su
// stderr; la nota è in grigio e può essere vuota. Come Warn non è gated da --quiet.
func WarnField(label, value, note string) {
	fmt.Fprintln(os.Stderr, renderField(warnStyle, "!", label, value, note))
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
// In modalità --json viene scritto su stderr per non sporcare stdout.
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
