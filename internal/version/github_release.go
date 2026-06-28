package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// GitHubReleaseProvider risolve la versione più recente da GitHub releases.
type GitHubReleaseProvider struct {
	Repo       string       // es. "BurntSushi/ripgrep"
	HTTPClient *http.Client // se nil usa http.DefaultClient
}

type githubReleaseResponse struct {
	TagName string `json:"tag_name"`
}

func (p GitHubReleaseProvider) Resolve(ctx context.Context) (string, string, error) {
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", p.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
	}

	var release githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("decode GitHub response: %w", err)
	}

	if release.TagName == "" {
		return "", "", fmt.Errorf("empty tag_name in GitHub response for %s", p.Repo)
	}

	tag := release.TagName
	ver := Clean(tag)
	return ver, tag, nil
}
