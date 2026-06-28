package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// archPackagesAPI is the official Arch Linux JSON API endpoint that returns
// packages from the official repos (core/extra) by exact name.
const archPackagesAPI = "https://archlinux.org/packages/search/json/"

// ArchLinuxProvider resolves the latest version of a package from the official
// Arch Linux repositories through the official JSON API.
//
// Compared to scraping the PKGBUILD, the JSON API returns pkgver already
// structured: no quotes to strip, no dynamic pkgver (pkgver=$(...)) and no
// variables/arrays to interpret.
//
// The strategy targets URL-based backends (which use {{version}}): it does not
// produce a release tag. The returned tag is the raw pkgver, best-effort.
type ArchLinuxProvider struct {
	Pkg        string       // Arch package name (e.g. "ripgrep", "maven")
	HTTPClient *http.Client // if nil, uses http.DefaultClient
}

type archPackagesResponse struct {
	Results []struct {
		PkgName string `json:"pkgname"`
		PkgVer  string `json:"pkgver"`
		PkgRel  string `json:"pkgrel"`
		Repo    string `json:"repo"`
	} `json:"results"`
}

func (p ArchLinuxProvider) Resolve(ctx context.Context) (string, string, error) {
	if p.Pkg == "" {
		return "", "", fmt.Errorf("arch-linux strategy: empty package name (set arch_pkg in the spec)")
	}

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	reqURL := archPackagesAPI + "?name=" + url.QueryEscape(p.Pkg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "paq")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("GET %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("Arch Linux API returned %d for %s", resp.StatusCode, reqURL)
	}

	var payload archPackagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", fmt.Errorf("decode Arch Linux response: %w", err)
	}

	if len(payload.Results) == 0 {
		return "", "", fmt.Errorf("package %q not found in official Arch repos", p.Pkg)
	}

	// Results differ only by architecture/repo: pkgver is the same.
	// Prefer the exact name match, falling back to the first result.
	raw := payload.Results[0].PkgVer
	for _, r := range payload.Results {
		if r.PkgName == p.Pkg {
			raw = r.PkgVer
			break
		}
	}

	if raw == "" {
		return "", "", fmt.Errorf("empty pkgver in Arch Linux response for %q", p.Pkg)
	}

	ver := Clean(raw)
	return ver, raw, nil
}
