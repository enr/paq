package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/enr/paq/internal/httpretry"
	"github.com/enr/paq/internal/jsonpath"
)

// JSONProvider resolves the latest version from a JSON document served over
// HTTP (strategy "json"), selecting it with a dot-separated path. It targets
// the projects that publish their current release only through an API, with no
// tags or repository to query.
//
// Pairing it with verify.sha256_url pointed at the same document is what makes
// a recipe self-consistent when the API only ever describes the current
// release: version and checksum then come from the same source instead of
// racing each other.
//
// The strategy targets URL-based backends (which use {{version}}): it does not
// produce a release tag. The returned tag is the raw selected value, best-effort.
type JSONProvider struct {
	URL        string       // document URL (not templated: it is fetched before the platform vars exist)
	Selector   string       // dot-separated path to the version inside the document
	HTTPClient *http.Client // if nil, uses a client with a 30s timeout
}

func (p JSONProvider) Resolve(ctx context.Context) (string, string, error) {
	if p.URL == "" {
		return "", "", fmt.Errorf("json strategy: empty document URL (set latest_url in the spec)")
	}
	if p.Selector == "" {
		return "", "", fmt.Errorf("json strategy: empty selector (set latest_json in the spec)")
	}

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "paq")

	resp, err := httpretry.Do(client, req)
	if err != nil {
		return "", "", fmt.Errorf("GET %s: %w", p.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("%s returned %d", p.URL, resp.StatusCode)
	}

	var doc any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", "", fmt.Errorf("decode %s: %w", p.URL, err)
	}

	raw, err := jsonpath.String(doc, p.Selector)
	if err != nil {
		return "", "", fmt.Errorf("%w in %s", err, p.URL)
	}
	if raw == "" {
		return "", "", fmt.Errorf("empty version at %q in %s", p.Selector, p.URL)
	}

	return Clean(raw), raw, nil
}
