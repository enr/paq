package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/enr/paq/internal/jsonpath"
)

// ParseSHA256JSON reads a JSON checksum document and returns the sha256 hash
// selected by selector, a dot-separated path through objects and arrays
// (e.g. "sha256hash", "assets.0.digest"). It covers the projects that publish
// checksums only through an API, with no checksum file next to the artifact.
//
// The selected value must be a string. Hashes are returned normalized: an
// algorithm prefix ("sha256:" or "sha256-", as used by OCI digests) is
// stripped and the digest is lowercased. Anything that is not a well-formed
// sha256 hex digest is an error, so a wrong selector is reported as such
// instead of surfacing later as a checksum mismatch.
func ParseSHA256JSON(checksumPath string, selector string) (string, error) {
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return "", fmt.Errorf("open checksum file %s: %w", checksumPath, err)
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse checksum file %s as JSON: %w", checksumPath, err)
	}

	s, err := jsonpath.String(doc, selector)
	if err != nil {
		return "", fmt.Errorf("%w in %s", err, checksumPath)
	}

	hash := normalizeSHA256(s)
	if !validSHA256(hash) {
		return "", fmt.Errorf("value at %q in %s is not a sha256 digest: %q", selector, checksumPath, s)
	}
	return hash, nil
}

// normalizeSHA256 trims the value and strips the algorithm prefix some APIs
// carry ("sha256:<hex>", "sha256-<hex>"), returning a lowercase digest.
func normalizeSHA256(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, prefix := range []string{"sha256:", "sha256-"} {
		if rest, found := strings.CutPrefix(s, prefix); found {
			return rest
		}
	}
	return s
}
