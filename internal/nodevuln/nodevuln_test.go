package nodevuln

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSatisfies(t *testing.T) {
	cases := []struct {
		rng, ver string
		want     bool
	}{
		{"22.x || 24.x", "24.1.0", true},
		{"22.x || 24.x", "20.1.0", false},
		{"20.5.x", "20.5.9", true},
		{"20.5.x", "20.6.0", false},
		{"^22.23.4 || ^24.22.1", "24.22.1", true},
		{"^22.23.4 || ^24.22.1", "24.22.0", false},
		{"^22.23.4 || ^24.22.1", "23.0.0", false},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{">= 8.0.0", "8.0.0", true},
		{">=4.0.0 <4.4.4", "4.4.3", true},
		{">=4.0.0 <4.4.4", "4.4.4", false},
		{"<= 8", "8.9.9", true},
		{"<= 8", "9.0.0", false},
		{"8.1.2", "8.1.2", true},
		{"", "1.0.0", false},
	}
	for _, c := range cases {
		ver, err := parseVersion(c.ver)
		if err != nil {
			t.Fatal(err)
		}
		got, err := satisfies(c.rng, ver)
		if err != nil {
			t.Errorf("satisfies(%q, %s): %v", c.rng, c.ver, err)
			continue
		}
		if got != c.want {
			t.Errorf("satisfies(%q, %s) = %v, want %v", c.rng, c.ver, got, c.want)
		}
	}
}

func TestSatisfiesRejectsUnknownSyntax(t *testing.T) {
	if _, err := satisfies("1.2.3 - 1.4.0", []int{1, 3, 0}); err == nil {
		t.Error("expected an error for a hyphen range")
	}
}

func TestCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
  "1": {"cve": ["CVE-1"], "vulnerable": "22.x || 24.x", "patched": "^22.2.0 || ^24.3.0", "affectedEnvironments": ["all"]},
  "2": {"cve": [], "vulnerable": "24.x", "patched": "^24.5.0", "affectedEnvironments": ["all"]},
  "10": {"cve": ["CVE-10a", "CVE-10b"], "vulnerable": "24.x", "patched": "", "affectedEnvironments": ["win32"]}
}`))
	}))
	defer srv.Close()
	old := IndexURL
	IndexURL = srv.URL
	defer func() { IndexURL = old }()

	got, err := Check(context.Background(), nil, "linux", []string{"24.1.0", "24.4.0", "22.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"NSWG-CORE-2", "CVE-1"}, {"NSWG-CORE-2"}, {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linux: got %v, want %v", got, want)
	}

	got, err = Check(context.Background(), nil, "windows", []string{"24.4.0"})
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"CVE-10a", "CVE-10b", "NSWG-CORE-2"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("windows: got %v, want %v", got, want)
	}
}
