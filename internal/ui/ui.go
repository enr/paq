package ui

import (
	"os"

	"golang.org/x/term"
)

// Config contiene le impostazioni UX globali, impostate da flag/env all'avvio.
type Config struct {
	NoColor bool
	JSON    bool
	Quiet   bool
	Verbose bool
	Debug   bool
}

// Global è la configurazione UX corrente, letta da root.go prima di eseguire i comandi.
var Global Config

// IsTTY ritorna true se stdout è collegato a un terminale interattivo.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// IsColorEnabled ritorna true se i colori sono abilitati
// (TTY presente, NO_COLOR non impostato, --no-color non specificato).
func IsColorEnabled() bool {
	if Global.NoColor {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return IsTTY()
}
