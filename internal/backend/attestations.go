package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/klauspost/compress/snappy"

	"github.com/enr/paq/internal/httpretry"
)

// SLSAProvenanceV1 is the predicate type of the build-provenance
// attestations produced by actions/attest-build-provenance.
const SLSAProvenanceV1 = "https://slsa.dev/provenance/v1"

// maxAttestations bounds how many attestations are fetched for one digest.
const maxAttestations = 30

// FetchAttestationBundles returns the Sigstore bundles (JSON) of the
// build-provenance attestations that repo ("owner/name") holds for the
// artifact with the given sha256 (hex). No attestation (HTTP 404 or an
// empty list) is not an error: it returns no bundles, and deciding that this
// fails verification is up to the caller.
//
// Like the gh CLI, a bundle is read from its bundle_url when present (a
// snappy-compressed JSON document on blob storage, fetched without the GitHub
// token), otherwise from the inline bundle field.
func FetchAttestationBundles(ctx context.Context, client *http.Client, repo, sha256hex string) ([][]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/attestations/sha256:%s?per_page=%d&predicate_type=%s",
		repo, sha256hex, maxAttestations, SLSAProvenanceV1)
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
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
	}

	var body struct {
		Attestations []struct {
			Bundle    json.RawMessage `json:"bundle"`
			BundleURL string          `json:"bundle_url"`
		} `json:"attestations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode attestations: %w", err)
	}

	var bundles [][]byte
	for _, a := range body.Attestations {
		switch {
		case a.BundleURL != "":
			b, err := fetchBundleURL(ctx, client, a.BundleURL)
			if err != nil {
				return nil, err
			}
			bundles = append(bundles, b)
		case len(a.Bundle) > 0 && string(a.Bundle) != "null":
			bundles = append(bundles, a.Bundle)
		}
	}
	return bundles, nil
}

// fetchBundleURL downloads and decompresses a snappy-compressed bundle.
func fetchBundleURL(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := httpretry.Do(client, req)
	if err != nil {
		return nil, fmt.Errorf("GET attestation bundle: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("attestation bundle: HTTP %d", resp.StatusCode)
	}
	compressed, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read attestation bundle: %w", err)
	}
	b, err := snappy.Decode(nil, compressed)
	if err != nil {
		return nil, fmt.Errorf("decompress attestation bundle: %w", err)
	}
	return b, nil
}
