package backend

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/enr/paq/internal/httpretry"
)

// NpmRegistry is the base URL of the public npm registry.
const NpmRegistry = "https://registry.npmjs.org"

// NpmBackend resolves the download URL of a native binary distributed on
// npm the way esbuild or biome do: the package lists one package per
// platform in its optionalDependencies, each declaring the os/cpu/libc it
// runs on, and the one matching the current platform holds the executable.
type NpmBackend struct {
	Package    string       // e.g. "esbuild", "@biomejs/biome"
	HTTPClient *http.Client // if nil, uses a client with a 30s timeout
}

// npmManifest is the part of a package version's manifest paq reads.
type npmManifest struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	OS                   []string          `json:"os"`
	CPU                  []string          `json:"cpu"`
	Libc                 []string          `json:"libc"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	Dist                 struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

// Resolve picks, among the optionalDependencies of the package at version,
// the platform package matching goos/goarch/env (paq's canonical values, as
// platform.Detect returns them) and returns its tarball URL and the sha512
// (hex) the registry records for it.
func (b NpmBackend) Resolve(ctx context.Context, version, goos, goarch, env string) (url, sha512hex string, err error) {
	client := b.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	root, err := fetchNpmManifest(ctx, client, b.Package, version)
	if err != nil {
		return "", "", err
	}
	if len(root.OptionalDependencies) == 0 {
		return "", "", fmt.Errorf("npm package %s@%s has no optionalDependencies: it ships no per-platform binaries", b.Package, version)
	}

	// Every candidate's manifest is needed to read its os/cpu/libc: fetch
	// them concurrently (esbuild has over 20).
	names := make([]string, 0, len(root.OptionalDependencies))
	for name := range root.OptionalDependencies {
		names = append(names, name)
	}
	sort.Strings(names)
	manifests := make([]npmManifest, len(names))
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pkg, ver := npmDependency(name, root.OptionalDependencies[name])
			manifests[i], errs[i] = fetchNpmManifest(ctx, client, pkg, ver)
		}()
	}
	wg.Wait()

	npmOS, npmCPU, libc := npmPlatform(goos, goarch, env)
	var matches []npmManifest
	for i, m := range manifests {
		if errs[i] != nil {
			return "", "", errs[i]
		}
		if npmFieldAllows(m.OS, npmOS) && npmFieldAllows(m.CPU, npmCPU) && (libc == "" || npmFieldAllows(m.Libc, libc)) {
			matches = append(matches, m)
		}
	}
	// A package with no libc field runs on every libc, so on musl both the
	// generic and the musl build match: the one declaring musl wins.
	if len(matches) > 1 && libc != "" {
		var explicit []npmManifest
		for _, m := range matches {
			if slices.Contains(m.Libc, libc) {
				explicit = append(explicit, m)
			}
		}
		if len(explicit) > 0 {
			matches = explicit
		}
	}

	platform := npmOS + "/" + npmCPU
	if libc != "" {
		platform += "/" + libc
	}
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("npm package %s@%s has no platform package for %s", b.Package, version, platform)
	case 1:
	default:
		found := make([]string, len(matches))
		for i, m := range matches {
			found[i] = m.Name
		}
		return "", "", fmt.Errorf("npm package %s@%s has %d platform packages for %s: %s", b.Package, version, len(matches), platform, strings.Join(found, ", "))
	}

	m := matches[0]
	sum, err := npmSHA512(m.Dist.Integrity)
	if err != nil {
		return "", "", fmt.Errorf("npm package %s@%s: %w", m.Name, m.Version, err)
	}
	return m.Dist.Tarball, sum, nil
}

// NpmPackageURL is the registry URL of the full metadata document of pkg,
// covering every version.
func NpmPackageURL(pkg string) string {
	// A scoped name keeps its "@", with the slash escaped as npm itself does.
	return NpmRegistry + "/" + strings.Replace(pkg, "/", "%2f", 1)
}

// NpmManifestURL is the registry URL of the manifest of pkg at version (a
// version or a dist-tag such as "latest").
func NpmManifestURL(pkg, version string) string {
	return NpmPackageURL(pkg) + "/" + version
}

// fetchNpmManifest gets the manifest of pkg at version from the registry.
func fetchNpmManifest(ctx context.Context, client *http.Client, pkg, version string) (npmManifest, error) {
	var m npmManifest
	url := NpmManifestURL(pkg, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return m, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpretry.Do(client, req)
	if err != nil {
		return m, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("npm registry returned %d for %s", resp.StatusCode, url)
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return m, fmt.Errorf("decode npm manifest %s: %w", url, err)
	}
	return m, nil
}

// npmDependency returns the package and version a dependency entry points
// to, following an alias ("npm:@scope/pkg@1.2.3").
func npmDependency(name, spec string) (pkg, version string) {
	alias, ok := strings.CutPrefix(spec, "npm:")
	if !ok {
		return name, spec
	}
	// The version follows the last "@" (a scoped name starts with one).
	if i := strings.LastIndex(alias, "@"); i > 0 {
		return alias[:i], alias[i+1:]
	}
	return alias, "latest"
}

// npmPlatform translates paq's canonical platform into the values npm uses
// in the os, cpu and libc fields. libc is empty off Linux.
func npmPlatform(goos, goarch, env string) (npmOS, cpu, libc string) {
	npmOS = goos
	if goos == "windows" {
		npmOS = "win32"
	}
	cpu = goarch
	switch goarch {
	case "amd64":
		cpu = "x64"
	case "386":
		cpu = "ia32"
	}
	switch env {
	case "gnu":
		libc = "glibc"
	case "musl":
		libc = "musl"
	}
	return npmOS, cpu, libc
}

// npmFieldAllows applies npm's rules for the os/cpu/libc fields: an empty
// list allows everything, a "!value" entry excludes value, and a list with
// positive entries allows only those.
func npmFieldAllows(field []string, value string) bool {
	positive := false
	allowed := false
	for _, f := range field {
		if neg, ok := strings.CutPrefix(f, "!"); ok {
			if neg == value {
				return false
			}
			continue
		}
		positive = true
		if f == value {
			allowed = true
		}
	}
	return !positive || allowed
}

// npmSHA512 converts a Subresource Integrity string ("sha512-<base64>") into
// a hex sha512.
func npmSHA512(integrity string) (string, error) {
	for _, h := range strings.Fields(integrity) {
		if b64, ok := strings.CutPrefix(h, "sha512-"); ok {
			sum, err := base64.StdEncoding.DecodeString(b64)
			if err != nil || len(sum) != 64 {
				return "", fmt.Errorf("invalid sha512 integrity %q", h)
			}
			return hex.EncodeToString(sum), nil
		}
	}
	return "", fmt.Errorf("no sha512 integrity in the registry metadata (got %q)", integrity)
}
