// Package nodevuln looks up the known vulnerabilities of Node.js runtime
// versions in the Node.js Security WG feed
// (https://github.com/nodejs/security-wg/tree/main/vuln/core), which OSV
// does not cover.
package nodevuln

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/enr/paq/internal/httpretry"
)

// IndexURL is the feed of Node.js core vulnerabilities. A var so tests can
// point it at a local server.
var IndexURL = "https://raw.githubusercontent.com/nodejs/security-wg/main/vuln/core/index.json"

// entry is a feed record: a version is affected when it is in Vulnerable
// and not in Patched (both npm-style semver ranges, e.g. "22.x || 24.x").
type entry struct {
	CVE                  []string `json:"cve"`
	Vulnerable           string   `json:"vulnerable"`
	Patched              string   `json:"patched"`
	AffectedEnvironments []string `json:"affectedEnvironments"`
}

// Check returns, for each version and in the same order, the IDs of the
// vulnerabilities affecting it on goos (empty when none is known), newest
// first. An entry is identified by its CVEs, or by "NSWG-CORE-<n>" (its key
// in the feed) when it has none. The feed is fetched once. A nil client uses
// one with a 30s timeout.
func Check(ctx context.Context, client *http.Client, goos string, versions []string) ([][]string, error) {
	if len(versions) == 0 {
		return nil, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, IndexURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "paq")
	resp, err := httpretry.Do(client, req)
	if err != nil {
		return nil, fmt.Errorf("node.js vulnerability feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node.js vulnerability feed returned %d", resp.StatusCode)
	}
	var feed map[string]entry
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode node.js vulnerability feed: %w", err)
	}

	keys := make([]string, 0, len(feed))
	for k := range feed {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keyNum(keys[i]) > keyNum(keys[j]) })

	ids := make([][]string, len(versions))
	for i, v := range versions {
		ver, err := parseVersion(v)
		if err == nil && len(ver) != 3 {
			err = fmt.Errorf("not a full version")
		}
		if err != nil {
			return nil, fmt.Errorf("node.js version %q: %w", v, err)
		}
		ids[i] = []string{}
		for _, k := range keys {
			e := feed[k]
			if !affectsOS(e.AffectedEnvironments, goos) {
				continue
			}
			hit, err := affects(e, ver)
			if err != nil {
				return nil, fmt.Errorf("node.js vulnerability feed entry %s: %w", k, err)
			}
			if !hit {
				continue
			}
			if len(e.CVE) == 0 {
				ids[i] = append(ids[i], "NSWG-CORE-"+k)
			} else {
				ids[i] = append(ids[i], e.CVE...)
			}
		}
	}
	return ids, nil
}

func keyNum(k string) int {
	n, _ := strconv.Atoi(k)
	return n
}

// affectsOS reports whether an entry's environments (Node's process.platform
// names, or "all") include goos.
func affectsOS(envs []string, goos string) bool {
	platform := goos
	switch goos {
	case "windows":
		platform = "win32"
	case "solaris":
		platform = "sunos"
	}
	for _, e := range envs {
		if e == "all" || e == platform {
			return true
		}
	}
	return len(envs) == 0
}

func affects(e entry, ver []int) (bool, error) {
	vulnerable, err := satisfies(e.Vulnerable, ver)
	if err != nil || !vulnerable {
		return false, err
	}
	patched, err := satisfies(e.Patched, ver)
	return !patched, err
}

// satisfies reports whether ver is in rng, the subset of npm semver ranges
// the feed uses: alternatives joined by "||", each a space-separated list of
// comparators: "1.2.3", "1.x", "1.2.x", "^1.2.3", "~1.2.3", or one of
// >=, >, <=, < followed by a full or partial version. An empty range matches
// nothing.
func satisfies(rng string, ver []int) (bool, error) {
	for _, alt := range strings.Split(rng, "||") {
		// Join an operator to its version (">= 1.2.3" -> ">=1.2.3").
		fields := strings.Fields(alt)
		var comps []string
		for i := 0; i < len(fields); i++ {
			f := fields[i]
			if strings.Trim(f, "<>=") == "" && i+1 < len(fields) {
				f += fields[i+1]
				i++
			}
			comps = append(comps, f)
		}
		if len(comps) == 0 {
			continue
		}
		all := true
		for _, c := range comps {
			ok, err := matches(c, ver)
			if err != nil {
				return false, err
			}
			all = all && ok
		}
		if all {
			return true, nil
		}
	}
	return false, nil
}

// matches checks ver against one comparator. A partial version compares only
// the parts it has, which gives npm's semantics ("<=8" is "<9.0.0", "1.x" is
// "=1").
func matches(comp string, ver []int) (bool, error) {
	op := comp[:len(comp)-len(strings.TrimLeft(comp, "<>=^~"))]
	want, err := parseVersion(strings.TrimSuffix(comp[len(op):], ".x"))
	if err != nil {
		return false, fmt.Errorf("range %q: %w", comp, err)
	}
	cmp := compare(ver[:len(want)], want)
	switch op {
	case "", "=":
		return cmp == 0, nil
	case ">=":
		return cmp >= 0, nil
	case ">":
		return cmp > 0, nil
	case "<=":
		return cmp <= 0, nil
	case "<":
		return cmp < 0, nil
	case "^":
		return cmp >= 0 && ver[0] == want[0], nil
	case "~":
		return cmp >= 0 && ver[0] == want[0] && (len(want) < 2 || ver[1] == want[1]), nil
	}
	return false, fmt.Errorf("range %q: unsupported operator %q", comp, op)
}

// parseVersion parses "1", "1.2" or "1.2.3" into its numeric parts.
func parseVersion(s string) ([]int, error) {
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return nil, fmt.Errorf("invalid version %q", s)
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid version %q", s)
		}
		out[i] = n
	}
	return out, nil
}

// compare compares two versions of the same length part by part.
func compare(a, b []int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
