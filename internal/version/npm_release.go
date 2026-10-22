package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/enr/paq/internal/backend"
	"github.com/enr/paq/internal/httpretry"
)

// NpmReleaseProvider resolves the latest version of an npm package: its
// "latest" dist-tag.
type NpmReleaseProvider struct {
	Package string // e.g. "esbuild", "@biomejs/biome"
	// MinimumAge, when set, restricts "latest" to a version published at
	// least this long ago: if the "latest" dist-tag is younger, the highest
	// older stable version not above it is used instead (as pnpm does).
	// Zero means no restriction.
	MinimumAge time.Duration
	HTTPClient *http.Client // if nil, uses a client with a 30s timeout
}

// npmPackument is the part of a package's full metadata document read to
// enforce MinimumAge. Only the full document carries "time": the abbreviated
// one npm clients use by default does not.
type npmPackument struct {
	DistTags struct {
		Latest string `json:"latest"`
	} `json:"dist-tags"`
	Versions map[string]struct{} `json:"versions"`
	// Time maps each version to its publish time. Not decoded as time.Time:
	// an unpublished package records an object under "unpublished".
	Time map[string]any `json:"time"`
}

// published returns the publish time of version, if recorded.
func (d npmPackument) published(version string) (time.Time, bool) {
	s, ok := d.Time[version].(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

func (p NpmReleaseProvider) Resolve(ctx context.Context) (string, string, error) {
	ver, tag, _, err := p.ResolveWithPending(ctx)
	return ver, tag, err
}

// ResolveWithPending is Resolve plus the "latest" version when it is held back
// by MinimumAge (nil otherwise).
func (p NpmReleaseProvider) ResolveWithPending(ctx context.Context) (string, string, *PendingRelease, error) {
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if p.MinimumAge <= 0 {
		// The manifest the dist-tag points to is far smaller than the full
		// document.
		var m struct {
			Version string `json:"version"`
		}
		if err := getNpmJSON(ctx, client, backend.NpmManifestURL(p.Package, "latest"), &m); err != nil {
			return "", "", nil, err
		}
		if m.Version == "" {
			return "", "", nil, fmt.Errorf("empty version for the latest dist-tag of npm package %s", p.Package)
		}
		return m.Version, m.Version, nil, nil
	}

	var doc npmPackument
	if err := getNpmJSON(ctx, client, backend.NpmPackageURL(p.Package), &doc); err != nil {
		return "", "", nil, err
	}
	latest := doc.DistTags.Latest
	if latest == "" {
		return "", "", nil, fmt.Errorf("npm package %s has no latest dist-tag", p.Package)
	}
	cutoff := time.Now().Add(-p.MinimumAge)
	published, ok := doc.published(latest)
	if !ok {
		return "", "", nil, fmt.Errorf("npm package %s: no publish time for %s", p.Package, latest)
	}
	if !published.After(cutoff) {
		return latest, latest, nil, nil
	}
	pending := &PendingRelease{Version: latest, PublishedAt: published, EligibleAt: published.Add(p.MinimumAge)}

	// Prereleases are never picked, and neither is anything above the
	// dist-tag (e.g. a next major published as a stable version but not
	// promoted to latest).
	best := ""
	for v := range doc.Versions {
		t, ok := doc.published(v)
		if !ok || t.After(cutoff) || strings.Contains(v, "-") || Compare(Clean(v), Clean(latest)) > 0 {
			continue
		}
		if best == "" || Compare(Clean(v), Clean(best)) > 0 {
			best = v
		}
	}
	if best == "" {
		return "", "", nil, fmt.Errorf("no version of npm package %s is older than %s (minimum_release_age)", p.Package, p.MinimumAge)
	}
	return best, best, pending, nil
}

// getNpmJSON GETs url from the npm registry and decodes the JSON response
// into out.
func getNpmJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpretry.Do(client, req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("npm registry returned %d for %s", resp.StatusCode, url)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}
