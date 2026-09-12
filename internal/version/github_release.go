package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/enr/paq/internal/httpretry"
)

// githubReleasesPerPage is the page size used when listing releases to
// enforce MinimumAge (the /releases/latest endpoint returns a single release
// and cannot be used for that).
const githubReleasesPerPage = 30

// githubReleasesMaxPages bounds how many pages of releases are scanned for
// one older than MinimumAge, so a repo with no eligible release in its recent
// history fails fast instead of paginating through its entire history.
const githubReleasesMaxPages = 10

// GitHubReleaseProvider resolves the latest version from GitHub releases.
type GitHubReleaseProvider struct {
	Repo string // e.g. "BurntSushi/ripgrep"
	// MinimumAge, when set, restricts "latest" to the newest release
	// published at least this long ago: drafts, prereleases and releases
	// younger than MinimumAge are skipped in favor of the newest one that
	// qualifies. Zero means no restriction (the plain "latest" release).
	MinimumAge time.Duration
	HTTPClient *http.Client // if nil, uses http.DefaultClient
}

type githubReleaseResponse struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

func (p GitHubReleaseProvider) Resolve(ctx context.Context) (string, string, error) {
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if p.MinimumAge <= 0 {
		return p.resolveLatest(ctx, client)
	}
	return p.resolveWithMinimumAge(ctx, client)
}

// resolveLatest resolves the plain "latest" release via the dedicated
// endpoint, which already excludes drafts and prereleases.
func (p GitHubReleaseProvider) resolveLatest(ctx context.Context, client *http.Client) (string, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", p.Repo)
	var release githubReleaseResponse
	if err := p.getJSON(ctx, client, url, &release); err != nil {
		return "", "", err
	}
	if release.TagName == "" {
		return "", "", fmt.Errorf("empty tag_name in GitHub response for %s", p.Repo)
	}
	return Clean(release.TagName), release.TagName, nil
}

// resolveWithMinimumAge scans releases newest-first, skipping drafts,
// prereleases and anything younger than MinimumAge, and returns the first
// (i.e. newest) one that qualifies.
func (p GitHubReleaseProvider) resolveWithMinimumAge(ctx context.Context, client *http.Client) (string, string, error) {
	cutoff := time.Now().Add(-p.MinimumAge)
	checked := 0

	for page := 1; page <= githubReleasesMaxPages; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d&page=%d", p.Repo, githubReleasesPerPage, page)
		var releases []githubReleaseResponse
		if err := p.getJSON(ctx, client, url, &releases); err != nil {
			return "", "", err
		}
		if len(releases) == 0 {
			break
		}

		for _, r := range releases {
			checked++
			if r.Draft || r.Prerelease || r.TagName == "" {
				continue
			}
			if !r.PublishedAt.After(cutoff) {
				return Clean(r.TagName), r.TagName, nil
			}
		}

		if len(releases) < githubReleasesPerPage {
			break
		}
	}

	return "", "", fmt.Errorf("no release of %s is older than %s (minimum_release_age; checked %d releases)", p.Repo, p.MinimumAge, checked)
}

// getJSON issues an authenticated GET against the GitHub API and decodes the
// JSON response into out.
func (p GitHubReleaseProvider) getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpretry.Do(client, req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}
