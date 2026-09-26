package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/enr/paq/internal/httpretry"
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

// maxAssetPages bounds the pagination walk (50 pages = 5000 assets, far beyond
// any real release), so an API that never returns a short page cannot keep the
// loop issuing requests forever.
const maxAssetPages = 50

// Resolve expands the Asset template, looks up the matching asset in the
// GitHub release identified by tag, and returns the asset's API URL.
func (b GitHubBackend) Resolve(ctx context.Context, tag string, v template.Vars) (string, error) {
	url, _, err := b.ResolveAsset(ctx, tag, v)
	return url, err
}

// ResolveAsset is Resolve that also returns the name of the matched asset.
// The expanded Asset template may be a glob (path.Match syntax: *, ?, [...]),
// for releases whose asset names carry a part the recipe cannot predict; it
// must then match exactly one asset of the release.
func (b GitHubBackend) ResolveAsset(ctx context.Context, tag string, v template.Vars) (url, name string, err error) {
	client := b.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	// Expand the template to get the name (or pattern) of the asset we're looking for.
	pattern, err := template.Resolve(b.Asset, v)
	if err != nil {
		return "", "", fmt.Errorf("resolve asset template: %w", err)
	}
	isGlob := strings.ContainsAny(pattern, "*?[")
	if _, err := path.Match(pattern, ""); err != nil {
		return "", "", fmt.Errorf("invalid asset pattern %q: %w", pattern, err)
	}

	releaseURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", b.Repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpretry.Do(client, req)
	if err != nil {
		return "", "", fmt.Errorf("GET %s: %w", releaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, releaseURL)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("decode GitHub response: %w", err)
	}

	matches := matchAssets(release.Assets, pattern)

	// The release's embedded "assets" array is capped at one page (100
	// entries). A full first page means there may be more: fetch further
	// pages of /releases/{id}/assets until one comes back short, or the
	// asset is found. A glob walks every page: a match on a later page
	// would make it ambiguous.
	if len(release.Assets) == assetsPerPage && (len(matches) == 0 || isGlob) {
		more, err := b.matchAssetsInLaterPages(ctx, client, release.ID, pattern, isGlob)
		if err != nil {
			return "", "", err
		}
		matches = append(matches, more...)
	}

	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("asset %q not found in release %s of %s", pattern, tag, b.Repo)
	case 1:
		return matches[0].URL, matches[0].Name, nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return "", "", fmt.Errorf("asset pattern %q matches %d assets in release %s of %s: %s", pattern, len(matches), tag, b.Repo, strings.Join(names, ", "))
	}
}

// matchAssets returns the assets whose name matches pattern (an exact name
// or a glob already validated by the caller).
func matchAssets(assets []githubAsset, pattern string) []githubAsset {
	var matches []githubAsset
	for _, asset := range assets {
		if ok, _ := path.Match(pattern, asset.Name); ok {
			matches = append(matches, asset)
		}
	}
	return matches
}

// matchAssetsInLaterPages walks /repos/{repo}/releases/{id}/assets starting
// at page 2 (page 1 is the embedded array already scanned by the caller),
// stopping at the first short page (fewer than assetsPerPage entries),
// maxAssetPages, or — unless all is set — the first page with a match.
func (b GitHubBackend) matchAssetsInLaterPages(ctx context.Context, client *http.Client, releaseID int64, pattern string, all bool) ([]githubAsset, error) {
	var matches []githubAsset
	for page := 2; page <= maxAssetPages; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/releases/%d/assets?per_page=%d&page=%d", b.Repo, releaseID, assetsPerPage, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := httpretry.Do(client, req)
		if err != nil {
			return nil, fmt.Errorf("GET %s: %w", url, err)
		}
		var assets []githubAsset
		decodeErr := json.NewDecoder(resp.Body).Decode(&assets)
		statusOK := resp.StatusCode == http.StatusOK
		resp.Body.Close()
		if !statusOK {
			return nil, fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode GitHub response: %w", decodeErr)
		}

		matches = append(matches, matchAssets(assets, pattern)...)
		if len(assets) < assetsPerPage || (len(matches) > 0 && !all) {
			return matches, nil
		}
	}
	return nil, fmt.Errorf("release %d of %s: no short page after %d pages of assets: giving up", releaseID, b.Repo, maxAssetPages)
}
