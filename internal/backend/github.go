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
	ID     int64         `json:"id"`
	Assets []githubAsset `json:"assets"`
}

// assetsPerPage is the GitHub API's page size for both the release's
// embedded "assets" array and the paginated /releases/{id}/assets endpoint.
const assetsPerPage = 100

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

	if assetURL, ok := findAsset(release.Assets, assetName); ok {
		return assetURL, nil
	}

	// The release's embedded "assets" array is capped at one page (100
	// entries). A full first page means there may be more: fetch further
	// pages of /releases/{id}/assets until one comes back short or the asset
	// is found.
	if len(release.Assets) == assetsPerPage {
		assetURL, err := b.findAssetInLaterPages(ctx, client, release.ID, assetName)
		if err != nil {
			return "", err
		}
		if assetURL != "" {
			return assetURL, nil
		}
	}

	return "", fmt.Errorf("asset %q not found in release %s of %s", assetName, tag, b.Repo)
}

// findAsset scans assets for one named name, returning its API URL.
func findAsset(assets []githubAsset, name string) (string, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset.URL, true
		}
	}
	return "", false
}

// findAssetInLaterPages walks /repos/{repo}/releases/{id}/assets starting at
// page 2 (page 1 is the embedded array already scanned by the caller),
// stopping at the first short page (fewer than assetsPerPage entries) or a match.
func (b GitHubBackend) findAssetInLaterPages(ctx context.Context, client *http.Client, releaseID int64, assetName string) (string, error) {
	for page := 2; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/releases/%d/assets?per_page=%d&page=%d", b.Repo, releaseID, assetsPerPage, page)
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
		var assets []githubAsset
		decodeErr := json.NewDecoder(resp.Body).Decode(&assets)
		statusOK := resp.StatusCode == http.StatusOK
		resp.Body.Close()
		if !statusOK {
			return "", fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
		}
		if decodeErr != nil {
			return "", fmt.Errorf("decode GitHub response: %w", decodeErr)
		}

		if assetURL, ok := findAsset(assets, assetName); ok {
			return assetURL, nil
		}
		if len(assets) < assetsPerPage {
			return "", nil
		}
	}
}
