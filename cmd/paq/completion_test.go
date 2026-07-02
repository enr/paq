package main

import (
	"reflect"
	"testing"

	"github.com/enr/paq/internal/state"
	"github.com/spf13/cobra"
)

func TestExcludeArgs(t *testing.T) {
	got := excludeArgs([]string{"rg", "bat", "delta"}, []string{"bat"})
	want := []string{"rg", "delta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("excludeArgs = %v, want %v", got, want)
	}

	// No exclusions: returns the original slice unchanged.
	got = excludeArgs([]string{"rg", "bat"}, nil)
	want = []string{"rg", "bat"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("excludeArgs(nil already) = %v, want %v", got, want)
	}
}

func TestFilterByPrefix(t *testing.T) {
	candidates := []string{"ripgrep", "runp", "rg", "bat"}

	got := filterByPrefix(candidates, "ri")
	want := []string{"ripgrep"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterByPrefix(ri) = %v, want %v", got, want)
	}

	got = filterByPrefix(candidates, "r")
	want = []string{"ripgrep", "runp", "rg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterByPrefix(r) = %v, want %v", got, want)
	}

	// Empty prefix: no filtering, returns everything.
	got = filterByPrefix(candidates, "")
	if !reflect.DeepEqual(got, candidates) {
		t.Errorf("filterByPrefix(\"\") = %v, want %v", got, candidates)
	}
}

func TestCompleteInstalledApps(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	st, err := state.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	st.Set(state.InstalledApp{Name: "rg", Version: "14.0.0"})
	st.Set(state.InstalledApp{Name: "rg", Version: "14.1.1"})
	st.Set(state.InstalledApp{Name: "bat", Version: "0.24.0"})
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	got, directive := completeInstalledApps(whichCmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", directive)
	}
	want := []string{"bat", "rg", "rg@14.0.0", "rg@14.1.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("completeInstalledApps = %v, want %v", got, want)
	}

	// Prefix filtering on the versioned form.
	got, _ = completeInstalledApps(whichCmd, nil, "rg@14.1")
	want = []string{"rg@14.1.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("completeInstalledApps(rg@14.1) = %v, want %v", got, want)
	}

	// Single-version app: no @version form offered.
	got, _ = completeInstalledApps(whichCmd, nil, "bat")
	want = []string{"bat"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("completeInstalledApps(bat) = %v, want %v", got, want)
	}
}

func TestCompleteRegistrySpecs(t *testing.T) {
	got, directive := completeRegistrySpecs(registryShowCmd, nil, "ripg")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", directive)
	}
	found := false
	for _, name := range got {
		if name == "ripgrep" {
			found = true
		}
		if len(name) < 4 || name[:4] != "ripg" {
			t.Errorf("completeRegistrySpecs(ripg) returned non-matching candidate %q", name)
		}
	}
	if !found {
		t.Error("expected \"ripgrep\" among the completions")
	}
}
