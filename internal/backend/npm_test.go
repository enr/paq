package backend

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// npmRegistryServer serves the given manifests, keyed by "name/version" as
// they appear in the (decoded) request path.
func npmRegistryServer(t *testing.T, manifests map[string]map[string]any) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := manifests[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(m)
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &rewriteTransport{base: srv.URL}}
}

func npmIntegrity(data string) string {
	sum := sha512.Sum512([]byte(data))
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

func npmPlatformPackage(name string, os, cpu, libc []string) map[string]any {
	m := map[string]any{
		"name":    name,
		"version": "1.0.0",
		"os":      os,
		"cpu":     cpu,
		"dist": map[string]string{
			"tarball":   "https://registry.npmjs.org/" + name + "/-/pkg-1.0.0.tgz",
			"integrity": npmIntegrity(name),
		},
	}
	if libc != nil {
		m["libc"] = libc
	}
	return m
}

// npmTestRegistry mimics biome: generic Linux builds plus musl ones, and
// Windows/macOS builds.
func npmTestRegistry(t *testing.T) *http.Client {
	return npmRegistryServer(t, map[string]map[string]any{
		"tool/1.0.0": {
			"name":    "tool",
			"version": "1.0.0",
			"optionalDependencies": map[string]string{
				"@tool/linux-x64":        "1.0.0",
				"@tool/linux-x64-musl":   "1.0.0",
				"@tool/linux-arm64":      "1.0.0",
				"@tool/darwin-arm64":     "1.0.0",
				"@tool/win32-x64":        "1.0.0",
				"@tool/linux-x64-legacy": "npm:@legacy/tool-linux@1.0.0",
			},
		},
		"@tool/linux-x64/1.0.0":      npmPlatformPackage("@tool/linux-x64", []string{"linux"}, []string{"x64"}, []string{"glibc"}),
		"@tool/linux-x64-musl/1.0.0": npmPlatformPackage("@tool/linux-x64-musl", []string{"linux"}, []string{"x64"}, []string{"musl"}),
		"@tool/linux-arm64/1.0.0":    npmPlatformPackage("@tool/linux-arm64", []string{"linux"}, []string{"arm64"}, nil),
		"@tool/darwin-arm64/1.0.0":   npmPlatformPackage("@tool/darwin-arm64", []string{"darwin"}, []string{"arm64"}, nil),
		"@tool/win32-x64/1.0.0":      npmPlatformPackage("@tool/win32-x64", []string{"win32"}, []string{"x64"}, nil),
		"@legacy/tool-linux/1.0.0":   npmPlatformPackage("@legacy/tool-linux", []string{"linux"}, []string{"ia32"}, nil),
	})
}

func TestNpmBackendResolve(t *testing.T) {
	client := npmTestRegistry(t)
	tests := []struct {
		goos, goarch, env string
		want              string
	}{
		{"linux", "amd64", "gnu", "@tool/linux-x64"},
		{"linux", "amd64", "musl", "@tool/linux-x64-musl"},
		// No libc field: runs on any libc.
		{"linux", "arm64", "musl", "@tool/linux-arm64"},
		{"darwin", "arm64", "", "@tool/darwin-arm64"},
		{"windows", "amd64", "", "@tool/win32-x64"},
		// Reached through an "npm:" alias.
		{"linux", "386", "gnu", "@legacy/tool-linux"},
	}
	for _, tt := range tests {
		b := NpmBackend{Package: "tool", HTTPClient: client}
		url, sum, err := b.Resolve(context.Background(), "1.0.0", tt.goos, tt.goarch, tt.env)
		if err != nil {
			t.Errorf("%s/%s/%s: %v", tt.goos, tt.goarch, tt.env, err)
			continue
		}
		if want := "https://registry.npmjs.org/" + tt.want + "/-/pkg-1.0.0.tgz"; url != want {
			t.Errorf("%s/%s/%s: url = %q, want %q", tt.goos, tt.goarch, tt.env, url, want)
		}
		h := sha512.Sum512([]byte(tt.want))
		if want := hex.EncodeToString(h[:]); sum != want {
			t.Errorf("%s/%s/%s: sha512 = %q, want %q", tt.goos, tt.goarch, tt.env, sum, want)
		}
	}
}

func TestNpmBackendNoPlatformPackage(t *testing.T) {
	b := NpmBackend{Package: "tool", HTTPClient: npmTestRegistry(t)}
	_, _, err := b.Resolve(context.Background(), "1.0.0", "darwin", "amd64", "")
	if err == nil || !strings.Contains(err.Error(), "no platform package for darwin/x64") {
		t.Errorf("err = %v, want a missing platform package error", err)
	}
}

func TestNpmBackendNoOptionalDependencies(t *testing.T) {
	client := npmRegistryServer(t, map[string]map[string]any{
		"plain/1.0.0": {"name": "plain", "version": "1.0.0"},
	})
	b := NpmBackend{Package: "plain", HTTPClient: client}
	_, _, err := b.Resolve(context.Background(), "1.0.0", "linux", "amd64", "gnu")
	if err == nil || !strings.Contains(err.Error(), "no optionalDependencies") {
		t.Errorf("err = %v, want a no optionalDependencies error", err)
	}
}

func TestNpmBackendUnknownVersion(t *testing.T) {
	b := NpmBackend{Package: "tool", HTTPClient: npmTestRegistry(t)}
	_, _, err := b.Resolve(context.Background(), "9.9.9", "linux", "amd64", "gnu")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want a 404 error", err)
	}
}

func TestNpmManifestURL(t *testing.T) {
	if got, want := NpmManifestURL("@biomejs/biome", "latest"), "https://registry.npmjs.org/@biomejs%2fbiome/latest"; got != want {
		t.Errorf("scoped: got %q, want %q", got, want)
	}
	if got, want := NpmManifestURL("esbuild", "0.1.0"), "https://registry.npmjs.org/esbuild/0.1.0"; got != want {
		t.Errorf("unscoped: got %q, want %q", got, want)
	}
}

func TestNpmFieldAllows(t *testing.T) {
	tests := []struct {
		field []string
		value string
		want  bool
	}{
		{nil, "linux", true},
		{[]string{"linux"}, "linux", true},
		{[]string{"linux"}, "darwin", false},
		{[]string{"!win32"}, "linux", true},
		{[]string{"!win32"}, "win32", false},
		{[]string{"linux", "!linux"}, "linux", false},
	}
	for _, tt := range tests {
		if got := npmFieldAllows(tt.field, tt.value); got != tt.want {
			t.Errorf("npmFieldAllows(%q, %q) = %v, want %v", tt.field, tt.value, got, tt.want)
		}
	}
}

func TestNpmSHA512(t *testing.T) {
	if _, err := npmSHA512("sha1-2jmj7l5rSw0yVb/vlWAYkK/YBwk="); err == nil {
		t.Error("sha1-only integrity: expected an error")
	}
	if _, err := npmSHA512("sha512-AAAA"); err == nil {
		t.Error("truncated sha512: expected an error")
	}
}
