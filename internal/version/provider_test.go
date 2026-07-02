package version

import (
	"context"
	"testing"
)

func TestPinProviderResolve(t *testing.T) {
	tests := []struct {
		version     string
		tagTemplate string
		wantVersion string
		wantTag     string
	}{
		{"14.1.1", "", "14.1.1", "v14.1.1"},
		{"v14.1.1", "", "14.1.1", "v14.1.1"},
		{"1.3.14", "bun-v{{version}}", "1.3.14", "bun-v1.3.14"},
		{"v1.3.14", "bun-v{{version}}", "1.3.14", "bun-v1.3.14"},
		{"21.0.2", "jdk-{{version}}", "21.0.2", "jdk-21.0.2"},
	}
	for _, tc := range tests {
		p := PinProvider{Version: tc.version, TagTemplate: tc.tagTemplate}
		ver, tag, err := p.Resolve(context.Background())
		if err != nil {
			t.Errorf("PinProvider{%q, %q}.Resolve() error: %v", tc.version, tc.tagTemplate, err)
			continue
		}
		if ver != tc.wantVersion || tag != tc.wantTag {
			t.Errorf("PinProvider{%q, %q}.Resolve() = (%q, %q), want (%q, %q)",
				tc.version, tc.tagTemplate, ver, tag, tc.wantVersion, tc.wantTag)
		}
	}
}

func TestPinProviderResolveBadTemplate(t *testing.T) {
	p := PinProvider{Version: "1.3.14", TagTemplate: "bun-{{unknown_placeholder}}"}
	if _, _, err := p.Resolve(context.Background()); err == nil {
		t.Error("expected error for unknown placeholder in tag template, got nil")
	}
}
