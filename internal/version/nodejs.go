package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/enr/paq/internal/httpretry"
)

// nodejsIndexURL is the official Node.js release index: every release, newest
// first by date, each with an "lts" field that is false or the LTS codename.
const nodejsIndexURL = "https://nodejs.org/dist/index.json"

// NodejsProvider resolves the newest Node.js LTS release (strategy "nodejs").
//
// The generic "json" strategy cannot do this: the index's first entry can be a
// prerelease (e.g. "v27.0.0-alpha.0.0.0") or a Current release, and since the
// index is ordered by date a maintenance LTS release can precede the newest LTS
// line, so the highest LTS version is picked rather than the first one.
type NodejsProvider struct {
	HTTPClient *http.Client // if nil, uses a client with a 30s timeout
}

func (p NodejsProvider) Resolve(ctx context.Context) (string, string, error) {
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodejsIndexURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "paq")

	resp, err := httpretry.Do(client, req)
	if err != nil {
		return "", "", fmt.Errorf("GET %s: %w", nodejsIndexURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("%s returned %d", nodejsIndexURL, resp.StatusCode)
	}

	var releases []struct {
		Version string `json:"version"`
		LTS     any    `json:"lts"` // false, or the codename string
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", "", fmt.Errorf("decode %s: %w", nodejsIndexURL, err)
	}

	var best, bestRaw string
	for _, r := range releases {
		if codename, ok := r.LTS.(string); !ok || codename == "" {
			continue
		}
		if v := Clean(r.Version); best == "" || Compare(v, best) > 0 {
			best, bestRaw = v, r.Version
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("no LTS release in %s", nodejsIndexURL)
	}
	return best, bestRaw, nil
}
