package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/enr/paq/internal/template"
)

// GitHubBackend resolves the download URL from GitHub releases.
type GitHubBackend struct {
	Repo       string       // e.g. "BurntSushi/ripgrep"
	Asset      string       // asset name template, e.g. "ripgrep-{{version}}-{{rust_target}}.tar.gz"
	HTTPClient *http.Client // if nil, uses http.DefaultClient
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"` // API asset URL, downloadable with the token even on private repos
}

type githubRelease struct {
	Assets []githubAsset `json:"assets"`
}

// Resolve expands the Asset template, looks up the asset with that name in the
// GitHub release identified by tag, and returns the asset's API URL.
func (b GitHubBackend) Resolve(ctx context.Context, tag string, v template.Vars) (string, error) {
	client := b.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	// Expand the template to get the name of the asset we're looking for.
	assetName, err := template.Resolve(b.Asset, v)
	if err != nil {
		return "", fmt.Errorf("resolve asset template: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", b.Repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode GitHub response: %w", err)
	}

	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return asset.URL, nil
		}
	}

	return "", fmt.Errorf("asset %q not found in release %s of %s", assetName, tag, b.Repo)
}
