// Package osv queries the OSV vulnerability database (https://osv.dev) for the
// known vulnerabilities affecting given package versions.
package osv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BatchURL is the OSV batch query endpoint. A var so tests can point it at a
// local server.
var BatchURL = "https://api.osv.dev/v1/querybatch"

// Query identifies a package version in an OSV ecosystem (e.g. "npm",
// "crates.io", "Go").
type Query struct {
	Ecosystem string
	Package   string
	Version   string
}

type batchRequest struct {
	Queries []batchQuery `json:"queries"`
}

type batchQuery struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	} `json:"package"`
	Version string `json:"version"`
}

type batchResponse struct {
	Results []struct {
		Vulns []struct {
			ID string `json:"id"`
		} `json:"vulns"`
	} `json:"results"`
}

// QueryBatch returns, for each query and in the same order, the IDs of the
// vulnerabilities affecting it (empty when none is known). All queries go in
// a single request. A nil client uses one with a 30s timeout.
//
// The request is not retried: httpretry only replays body-less GETs.
func QueryBatch(ctx context.Context, client *http.Client, queries []Query) ([][]string, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	body := batchRequest{Queries: make([]batchQuery, len(queries))}
	for i, q := range queries {
		body.Queries[i].Package.Ecosystem = q.Ecosystem
		body.Queries[i].Package.Name = q.Package
		body.Queries[i].Version = q.Version
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode osv query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BatchURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "paq")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("osv query: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("osv api returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}

	var out batchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode osv response: %w", err)
	}
	if len(out.Results) != len(queries) {
		return nil, fmt.Errorf("osv api returned %d results for %d queries", len(out.Results), len(queries))
	}

	ids := make([][]string, len(queries))
	for i, r := range out.Results {
		ids[i] = make([]string, len(r.Vulns))
		for j, v := range r.Vulns {
			ids[i][j] = v.ID
		}
	}
	return ids, nil
}
