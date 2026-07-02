package version

import (
	"context"
	"fmt"
	"strings"

	"github.com/enr/paq/internal/template"
)

// Provider resolves the version for an app.
type Provider interface {
	Resolve(ctx context.Context) (version string, tag string, err error)
}

// PinProvider always returns the configured version (pinned version).
type PinProvider struct {
	Version     string // e.g. "21.0.2" or "v21.0.2"
	TagTemplate string // optional tag template, e.g. "bun-v{{version}}"; empty = "v" + version
}

func (p PinProvider) Resolve(_ context.Context) (string, string, error) {
	clean := Clean(p.Version)
	if p.TagTemplate != "" {
		major, minor, patch := Parse(clean)
		tag, err := template.Resolve(p.TagTemplate, template.Vars{
			Version:      clean,
			VersionMajor: major,
			VersionMinor: minor,
			VersionPatch: patch,
		})
		if err != nil {
			return "", "", fmt.Errorf("resolve tag template: %w", err)
		}
		return clean, tag, nil
	}
	// GitHub tags usually have a "v" prefix, but some repos don't.
	// Use the original string if it starts with "v", otherwise add "v".
	tag := p.Version
	if !strings.HasPrefix(tag, "v") && !strings.HasPrefix(tag, "V") {
		tag = "v" + clean
	}
	return clean, tag, nil
}
