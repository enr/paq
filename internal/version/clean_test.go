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
