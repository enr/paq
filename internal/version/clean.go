package version

import (
	"regexp"
	"strconv"
	"strings"
)

var semverRe = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

// Clean removes the "v" prefix and any non-numeric suffix from a version string.
// Examples: "v14.1.1" → "14.1.1", "jdk-21.0.2+13" → "21.0.2", "14.1.1" → "14.1.1"
func Clean(raw string) string {
	// Remove the "v" prefix, case-insensitive.
	s := strings.TrimPrefix(raw, "v")
	s = strings.TrimPrefix(s, "V")

	// Extract the first N.N or N.N.N sequence.
	match := semverRe.FindString(s)
	if match != "" {
		return match
	}

	// Fallback: return the string without the "v" prefix.
	return s
}

// Build extracts the build number from a tag/version, i.e. the part after
// the first "+". Examples: "jdk-21.0.11+10" → "10", "21.0.2+13" → "13", "14.1.1" → "".
func Build(raw string) string {
	if idx := strings.IndexByte(raw, '+'); idx >= 0 {
		return raw[idx+1:]
	}
	return ""
}

// Compare compares two already-clean versions (e.g. "14.1.1") numerically,
// field by field (major, minor, patch). Returns <0 if a < b, 0 if equal,
// >0 if a > b. Missing or non-numeric fields count as 0.
func Compare(a, b string) int {
	aMajor, aMinor, aPatch := Parse(a)
	bMajor, bMinor, bPatch := Parse(b)
	if d := compareNumeric(aMajor, bMajor); d != 0 {
		return d
	}
	if d := compareNumeric(aMinor, bMinor); d != 0 {
		return d
	}
	return compareNumeric(aPatch, bPatch)
}

func compareNumeric(a, b string) int {
	ai, _ := strconv.Atoi(a)
	bi, _ := strconv.Atoi(b)
	return ai - bi
}

// Parse extracts major, minor, patch from an already-clean version (e.g. "14.1.1").
// If a field is not present, returns an empty string for that field.
func Parse(version string) (major, minor, patch string) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) >= 1 {
		major = parts[0]
	}
	if len(parts) >= 2 {
		minor = parts[1]
	}
	if len(parts) >= 3 {
		// Remove any suffix after the patch (e.g. "2+13" → "2").
		patch = semverRe.FindString(parts[2])
		if patch == "" {
			patch = parts[2]
		}
		// patch is just the number, must not contain dots.
		if idx := strings.IndexByte(patch, '.'); idx >= 0 {
			patch = patch[:idx]
		}
	}
	return
}
