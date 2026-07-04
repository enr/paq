package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
)

func TestEvaluateOutdated(t *testing.T) {
	cases := []struct {
		name      string
		appName   string
		installed []state.InstalledApp
		latest    string
		wantEntry ui.OutdatedEntry
		wantIsOld bool
	}{
		{
			name:      "up to date",
			appName:   "rg",
			installed: []state.InstalledApp{{Name: "rg", Version: "14.1.1"}},
			latest:    "14.1.1",
			wantIsOld: false,
		},
		{
			name:      "outdated single version",
			appName:   "rg",
			installed: []state.InstalledApp{{Name: "rg", Version: "14.0.0"}},
			latest:    "14.1.1",
			wantEntry: ui.OutdatedEntry{Name: "rg", Installed: "14.0.0", Latest: "14.1.1"},
			wantIsOld: true,
		},
		{
			name:    "outdated multiple installed versions, none matching",
			appName: "node18",
			installed: []state.InstalledApp{
				{Name: "node18", Version: "18.20.0"},
				{Name: "node18", Version: "18.19.0"},
			},
			latest:    "18.20.1",
			wantEntry: ui.OutdatedEntry{Name: "node18", Installed: "18.20.0, 18.19.0", Latest: "18.20.1"},
			wantIsOld: true,
		},
		{
			name:    "up to date when any installed version matches latest",
			appName: "node18",
			installed: []state.InstalledApp{
				{Name: "node18", Version: "18.19.0"},
				{Name: "node18", Version: "18.20.1"},
			},
			latest:    "18.20.1",
			wantIsOld: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, isOutdated := evaluateOutdated(tc.appName, tc.installed, tc.latest)
			if isOutdated != tc.wantIsOld {
				t.Fatalf("isOutdated = %v, want %v", isOutdated, tc.wantIsOld)
			}
			if !isOutdated {
				return
			}
			if entry != tc.wantEntry {
				t.Errorf("entry = %+v, want %+v", entry, tc.wantEntry)
			}
		})
	}
}

func TestCheckOutdatedSkipsPinnedVersion(t *testing.T) {
	cfg := &config.Config{
		Apps: map[string]config.AppEntry{
			"rg": {Use: "ripgrep", Version: "14.1.1"},
		},
		Specs: map[string]config.Spec{
			"ripgrep": {},
		},
	}
	st := &state.State{}

	var skipped []string
	skip := func(format string, a ...any) { skipped = append(skipped, fmt.Sprintf(format, a...)) }

	_, checked, err := checkOutdated(context.Background(), cfg, st, "rg", skip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checked {
		t.Error("expected pinned app to be skipped (not checked)")
	}
	if len(skipped) != 1 {
		t.Fatalf("expected exactly one skip message, got %v", skipped)
	}
}

func TestCheckOutdatedSkipsNotInstalled(t *testing.T) {
	cfg := &config.Config{
		Apps: map[string]config.AppEntry{
			"rg": {Use: "ripgrep", Version: "latest"},
		},
		Specs: map[string]config.Spec{
			"ripgrep": {},
		},
	}
	st := &state.State{} // nothing installed

	var skipped []string
	skip := func(format string, a ...any) { skipped = append(skipped, fmt.Sprintf(format, a...)) }

	_, checked, err := checkOutdated(context.Background(), cfg, st, "rg", skip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checked {
		t.Error("expected not-installed app to be skipped (not checked)")
	}
	if len(skipped) != 1 {
		t.Fatalf("expected exactly one skip message, got %v", skipped)
	}
}

func TestCheckOutdatedErrorsOnMissingSpec(t *testing.T) {
	cfg := &config.Config{
		Apps: map[string]config.AppEntry{
			"rg": {Use: "ripgrep", Version: "latest"},
		},
		Specs: map[string]config.Spec{},
	}
	st := &state.State{}
	st.Set(state.InstalledApp{Name: "rg", Version: "14.0.0"})

	_, _, err := checkOutdated(context.Background(), cfg, st, "rg", func(string, ...any) {})
	if err == nil {
		t.Fatal("expected error for missing spec, got nil")
	}
}

func TestCheckOutdatedNoLatestStrategySkips(t *testing.T) {
	// backend "url" with no latest_strategy cannot resolve "latest": resolveLatestVersion
	// fails with version.ErrLatestNotImplemented without any network call, exercising the
	// "no upstream strategy" skip branch deterministically.
	cfg := &config.Config{
		Apps: map[string]config.AppEntry{
			"mytool": {Use: "mytool", Version: "latest"},
		},
		Specs: map[string]config.Spec{
			"mytool": {Backend: "url", Source: "https://example.com/{{version}}.tar.gz"},
		},
	}
	st := &state.State{}
	st.Set(state.InstalledApp{Name: "mytool", Version: "1.0.0"})

	var skipped []string
	skip := func(format string, a ...any) { skipped = append(skipped, fmt.Sprintf(format, a...)) }

	_, checked, err := checkOutdated(context.Background(), cfg, st, "mytool", skip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checked {
		t.Error("expected app with no latest strategy to be skipped (not checked)")
	}
	if len(skipped) != 1 {
		t.Fatalf("expected exactly one skip message, got %v", skipped)
	}
}
