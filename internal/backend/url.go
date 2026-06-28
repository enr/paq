package backend

import (
	"github.com/enr/paq/internal/template"
)

// URLBackend risolve l'URL di download da un template.
type URLBackend struct {
	Source string // template URL, es. "https://example.com/jdk-{{version}}_{{os}}.tar.gz"
}

// Resolve espande il template Source con le variabili fornite e ritorna l'URL finale.
func (b URLBackend) Resolve(v template.Vars) (string, error) {
	return template.Resolve(b.Source, v)
}
