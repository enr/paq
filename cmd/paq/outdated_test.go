package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/ui"
	"github.com/spf13/cobra"
)

// cmdWithContext returns a throwaway command carrying a non-nil context:
// outdatedCmd (like other package-level commands) only gets one from cobra's
// own Execute machinery, which these tests bypass by calling runOutdated directly.
func cmdWithContext() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return cmd
}

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

// outdatedManifest builds a manifest with one app tracking "latest" via the
// "json" strategy against srv, so its "latest" resolution is fully offline
// and deterministic.
func outdatedManifest(latestURL string) string {
	return fmt.Sprintf(`[apps.tool]
use = "tool"
version = "latest"

[specs.tool]
backend = "url"
source = "https://example.com/tool-{{version}}.tar.gz"
archive = "tar.gz"
latest_strategy = "json"
latest_url = %q
latest_json = "version"
`, latestURL)
}

func TestRunOutdatedNoAppsConfigured(t *testing.T) {
	doctorEnv(t, "")

	out := captureStdout(t, func() {
		if err := runOutdated(cmdWithContext(), nil); err != nil {
			t.Fatalf("runOutdated: %v", err)
		}
	})
	if !strings.Contains(out, "No apps configured") {
		t.Errorf("output = %q, want the no-apps-configured message", out)
	}
}

func TestRunOutdatedNoAppsConfiguredJSON(t *testing.T) {
	doctorEnv(t, "")

	out := withJSON(t, func() {
		if err := runOutdated(cmdWithContext(), nil); err != nil {
			t.Fatalf("runOutdated: %v", err)
		}
	})
	var got []ui.OutdatedEntry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput:\n%s", err, out)
	}
	if len(got) != 0 {
		t.Errorf("got = %v, want an empty array", got)
	}
}

func TestRunOutdatedReportsOutdatedApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"2.0.0"}`))
	}))
	defer srv.Close()
	doctorEnv(t, outdatedManifest(srv.URL))

	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Set(state.InstalledApp{Name: "tool", Version: "1.0.0"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runOutdated(cmdWithContext(), nil); err != nil {
			t.Fatalf("runOutdated: %v", err)
		}
	})
	for _, want := range []string{"tool", "1.0.0", "2.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunOutdatedMissingSpecDoesNotHideOtherApps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"2.0.0"}`))
	}))
	defer srv.Close()
	doctorEnv(t, outdatedManifest(srv.URL)+`
[apps.broken]
use = "no-such-spec"
version = "latest"
`)

	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Set(state.InstalledApp{Name: "tool", Version: "1.0.0"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runOutdated(cmdWithContext(), nil)
	})
	if runErr == nil || !strings.Contains(runErr.Error(), `broken: spec "no-such-spec" not found in registry`) {
		t.Errorf("err = %v, want the missing-spec error for broken", runErr)
	}
	for _, want := range []string{"tool", "1.0.0", "2.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunOutdatedAllUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.0.0"}`))
	}))
	defer srv.Close()
	doctorEnv(t, outdatedManifest(srv.URL))

	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Set(state.InstalledApp{Name: "tool", Version: "1.0.0"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runOutdated(cmdWithContext(), nil); err != nil {
			t.Fatalf("runOutdated: %v", err)
		}
	})
	if !strings.Contains(out, "up to date") {
		t.Errorf("output = %q, want the up-to-date message", out)
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
