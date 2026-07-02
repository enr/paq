package backend

import (
	"github.com/enr/paq/internal/template"
)

// URLBackend resolves the download URL from a template.
type URLBackend struct {
	Source string // URL template, e.g. "https://example.com/jdk-{{version}}_{{os}}.tar.gz"
}

// Resolve expands the Source template with the given variables and returns the final URL.
func (b URLBackend) Resolve(v template.Vars) (string, error) {
	return template.Resolve(b.Source, v)
}
