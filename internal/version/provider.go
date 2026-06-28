package version

import (
	"context"
	"strings"
)

// Provider risolve la versione per un'app.
type Provider interface {
	Resolve(ctx context.Context) (version string, tag string, err error)
}

// PinProvider ritorna sempre la versione configurata (versione pinnata).
type PinProvider struct {
	Version string // es. "21.0.2" oppure "v21.0.2"
}

func (p PinProvider) Resolve(_ context.Context) (string, string, error) {
	clean := Clean(p.Version)
	// Il tag su GitHub di solito ha il prefisso "v", ma alcune repo non ce l'hanno.
	// Usiamo la stringa originale se inizia con "v", altrimenti aggiungiamo "v".
	tag := p.Version
	if !strings.HasPrefix(tag, "v") && !strings.HasPrefix(tag, "V") {
		tag = "v" + clean
	}
	return clean, tag, nil
}
