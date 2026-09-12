package version

import (
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"0h", 0},
		{"7d", 7 * 24 * time.Hour},
		{"6mo", 6 * 30 * 24 * time.Hour},
		{"1y", 365 * 24 * time.Hour},
	}
	for _, c := range cases {
		got, err := ParseAge(c.in)
		if err != nil {
			t.Errorf("ParseAge(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseAge(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseAgeInvalid(t *testing.T) {
	for _, in := range []string{"", "7", "d7", "1w", "7 d", "-1d", "1.5d"} {
		if _, err := ParseAge(in); err == nil {
			t.Errorf("ParseAge(%q): expected error, got nil", in)
		}
	}
}

func TestResolveMinimumAge(t *testing.T) {
	cases := []struct {
		name         string
		spec         string
		defaults     string
		wantAge      time.Duration
		wantExplicit bool
	}{
		{"neither set: built-in default", "", "", DefaultMinimumReleaseAge, false},
		{"only global default", "", "6mo", 6 * 30 * 24 * time.Hour, true},
		{"spec overrides global default", "7d", "6mo", 7 * 24 * time.Hour, true},
		{"spec alone", "7d", "", 7 * 24 * time.Hour, true},
		{"explicit zero disables", "0h", "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			age, explicit, err := ResolveMinimumAge(c.spec, c.defaults)
			if err != nil {
				t.Fatalf("ResolveMinimumAge error: %v", err)
			}
			if age != c.wantAge {
				t.Errorf("age = %v, want %v", age, c.wantAge)
			}
			if explicit != c.wantExplicit {
				t.Errorf("explicit = %v, want %v", explicit, c.wantExplicit)
			}
		})
	}
}

func TestResolveMinimumAgeInvalid(t *testing.T) {
	if _, _, err := ResolveMinimumAge("not-a-duration", ""); err == nil {
		t.Error("expected error for invalid spec value, got nil")
	}
	if _, _, err := ResolveMinimumAge("", "not-a-duration"); err == nil {
		t.Error("expected error for invalid defaults value, got nil")
	}
}
