package version

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// DefaultMinimumReleaseAge is the built-in minimum release age enforced when
// resolving "latest" if neither the spec nor the user's [defaults] section
// (minimum_release_age) configures one.
const DefaultMinimumReleaseAge = 24 * time.Hour

// ageRe matches a plain number followed by one of paq's release-age units.
var ageRe = regexp.MustCompile(`^(\d+)(h|d|mo|y)$`)

// ParseAge parses a release-age duration in paq's vocabulary: a number
// followed by a unit - h (hours), d (days), mo (months, 30 days) or y (years,
// 365 days) - e.g. "24h", "7d", "6mo", "1y". "0" in any unit means no minimum.
func ParseAge(s string) (time.Duration, error) {
	m := ageRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q: expected a number followed by h, d, mo or y (e.g. \"24h\", \"7d\", \"6mo\", \"1y\")", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}

	var unit time.Duration
	switch m[2] {
	case "h":
		unit = time.Hour
	case "d":
		unit = 24 * time.Hour
	case "mo":
		unit = 30 * 24 * time.Hour
	case "y":
		unit = 365 * 24 * time.Hour
	}
	return time.Duration(n) * unit, nil
}

// ResolveMinimumAge picks the minimum release age to enforce when resolving
// "latest": the spec's own override (specValue), else the user's global
// default ([defaults] minimum_release_age, defaultsValue), else the built-in
// default. explicit reports whether specValue or defaultsValue was set (as
// opposed to falling back to the built-in default), which callers use to
// decide whether an unsupported backend/strategy deserves a warning.
func ResolveMinimumAge(specValue, defaultsValue string) (age time.Duration, explicit bool, err error) {
	raw := specValue
	if raw == "" {
		raw = defaultsValue
	}
	if raw == "" {
		return DefaultMinimumReleaseAge, false, nil
	}
	age, err = ParseAge(raw)
	return age, true, err
}
