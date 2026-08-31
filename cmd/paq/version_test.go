package main

import (
	"testing"

	"github.com/enr/paq/internal/registry"
)

// withVersion sets the build-time paq version for the duration of a test.
func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := Version
	Version = v
	t.Cleanup(func() { Version = prev })
}

func TestRegistryIsStale(t *testing.T) {
	cases := []struct {
		name       string
		paqVersion string
		meta       *registry.Meta
		want       bool
	}{
		{"older snapshot", "0.0.13", &registry.Meta{Tag: "v0.0.12", Version: "0.0.12"}, true},
		{"same version", "0.0.13", &registry.Meta{Tag: "v0.0.13", Version: "0.0.13"}, false},
		{"newer snapshot", "0.0.13", &registry.Meta{Tag: "v0.0.14", Version: "0.0.14"}, false},
		{"v-prefixed", "v0.0.13", &registry.Meta{Tag: "v0.0.12", Version: "v0.0.12"}, true},
		{"snapshot build", "0.0.14-SNAPSHOT", &registry.Meta{Tag: "v0.0.13", Version: "0.0.13"}, true},
		// A custom registry has its own version line: never compared with paq's.
		{"custom source", "0.0.13", &registry.Meta{Tag: "custom", Version: "0.0.1"}, false},
		// A source build has no release version to compare against.
		{"dev build", "dev", &registry.Meta{Tag: "v0.0.12", Version: "0.0.12"}, false},
		{"no snapshot", "0.0.13", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withVersion(t, c.paqVersion)
			if got := registryIsStale(c.meta); got != c.want {
				t.Errorf("registryIsStale(%+v) with paq %s = %v, want %v", c.meta, c.paqVersion, got, c.want)
			}
		})
	}
}
