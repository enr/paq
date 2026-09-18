package version

import "testing"

func TestClean(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v14.1.1", "14.1.1"},
		{"14.1.1", "14.1.1"},
		{"V14.1.1", "14.1.1"},
		{"21.0.2", "21.0.2"},
		{"jdk-21.0.2+13", "21.0.2"},
		{"v1.2", "1.2"},
	}
	for _, tc := range tests {
		got := Clean(tc.input)
		if got != tc.want {
			t.Errorf("Clean(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"jdk-21.0.11+10", "10"},
		{"21.0.2+13", "13"},
		{"14.1.1", ""},
		{"jdk-21.0.3+9.1", "9.1"},
	}
	for _, tc := range tests {
		got := Build(tc.input)
		if got != tc.want {
			t.Errorf("Build(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int // sign of the expected result
	}{
		// Equal.
		{"14.1.1", "14.1.1", 0},
		{"1.2", "1.2", 0},
		{"0.0.0", "0.0.0", 0},

		// Major wins over minor and patch.
		{"2.0.0", "1.9.9", +1},
		{"1.9.9", "2.0.0", -1},

		// Minor wins over patch.
		{"1.2.0", "1.1.9", +1},
		{"1.1.9", "1.2.0", -1},

		// Patch.
		{"1.1.2", "1.1.1", +1},
		{"1.1.1", "1.1.2", -1},

		// Numeric, not lexicographic: the bug this guards against is "10"
		// sorting before "9" as a string.
		{"1.10.0", "1.9.0", +1},
		{"0.0.16", "0.0.9", +1},
		{"10.0.0", "9.0.0", +1},

		// A missing field counts as 0, so "1.2" == "1.2.0".
		{"1.2", "1.2.0", 0},
		{"1.2", "1.2.1", -1},
		{"1", "1.0.0", 0},

		// Non-numeric fields count as 0 rather than erroring.
		{"1.x.3", "1.0.3", 0},
		{"", "0.0.0", 0},
	}
	for _, tc := range tests {
		got := Compare(tc.a, tc.b)
		if sign(got) != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
		// Compare must be antisymmetric, or the call sites that branch on
		// "is the remote newer" and "is the local older" disagree.
		if rev := Compare(tc.b, tc.a); sign(rev) != -tc.want {
			t.Errorf("Compare(%q, %q) = %d, want sign %d (antisymmetry)", tc.b, tc.a, rev, -tc.want)
		}
	}
}

// Compare only looks at major/minor/patch: build metadata is not a field it
// reads. That is consistent by construction, because every call site runs
// Clean first and Clean strips the build suffix — two releases that differ
// only by build number are indistinguishable to Compare.
func TestCompareIgnoresBuildMetadata(t *testing.T) {
	if got := Compare(Clean("jdk-21.0.2+13"), Clean("jdk-21.0.2+9")); got != 0 {
		t.Errorf("Compare on build-only difference = %d, want 0", got)
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return +1
	default:
		return 0
	}
}

func TestParse(t *testing.T) {
	major, minor, patch := Parse("14.1.1")
	if major != "14" || minor != "1" || patch != "1" {
		t.Errorf("Parse(14.1.1) = %q %q %q, want 14 1 1", major, minor, patch)
	}

	major, minor, patch = Parse("21.0.2")
	if major != "21" || minor != "0" || patch != "2" {
		t.Errorf("Parse(21.0.2) = %q %q %q, want 21 0 2", major, minor, patch)
	}

	major, minor, patch = Parse("1.2")
	if major != "1" || minor != "2" || patch != "" {
		t.Errorf("Parse(1.2) = %q %q %q, want 1 2 ''", major, minor, patch)
	}
}
