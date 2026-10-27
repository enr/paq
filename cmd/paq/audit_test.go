package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/enr/paq/internal/osv"
	"github.com/enr/paq/internal/state"
)

// auditSetup seeds the state with a recipe mapped to OSV (bat), one mapped
// through the npm backend (esbuild) and one with no recipe at all, and serves
// OSV answers from vulns (keyed by package name). It returns the packages the
// fake OSV server was asked about.
func auditSetup(t *testing.T, vulns map[string][]string) *[]string {
	t.Helper()
	dir := t.TempDir()
	withStateHome(t, dir)
	withConfigHome(t, dir)

	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Set(state.InstalledApp{Name: "bat", Version: "0.18.0", Kind: "file", Dest: dir + "/bat"})
	st.Set(state.InstalledApp{Name: "esbuild", Version: "0.25.0", Kind: "file", Dest: dir + "/esbuild"})
	st.Set(state.InstalledApp{Name: "unmapped", Version: "1.0.0", Kind: "file", Dest: dir + "/unmapped"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Queries []struct {
				Package struct{ Ecosystem, Name string } `json:"package"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		type vuln struct {
			ID string `json:"id"`
		}
		var resp struct {
			Results []struct {
				Vulns []vuln `json:"vulns"`
			} `json:"results"`
		}
		resp.Results = make([]struct {
			Vulns []vuln `json:"vulns"`
		}, len(req.Queries))
		for i, q := range req.Queries {
			asked = append(asked, q.Package.Ecosystem+":"+q.Package.Name)
			for _, id := range vulns[q.Package.Name] {
				resp.Results[i].Vulns = append(resp.Results[i].Vulns, vuln{ID: id})
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	withFlag(t, &osv.BatchURL, srv.URL)
	return &asked
}

func TestRunAuditReportsVulnerabilities(t *testing.T) {
	asked := auditSetup(t, map[string][]string{"bat": {"GHSA-p24j-h477-76q3", "RUSTSEC-2021-0106"}})

	var runErr error
	out := withJSON(t, func() { runErr = runAudit(cmdWithContext(), nil) })

	if !errors.Is(runErr, errVulnerable) || exitCodeFor(runErr) != exitVulnerable {
		t.Errorf("err = %v (exit %d), want errVulnerable (exit %d)", runErr, exitCodeFor(runErr), exitVulnerable)
	}
	// Only the mapped tools are sent to OSV.
	if want := []string{"crates.io:bat", "npm:esbuild"}; !reflect.DeepEqual(*asked, want) {
		t.Errorf("queried %v, want %v", *asked, want)
	}

	var entries []struct {
		Name    string   `json:"name"`
		Checked bool     `json:"checked"`
		Vulns   []string `json:"vulns"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("audit --json is not valid JSON: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.Name] = strings.Join(e.Vulns, ",")
		if e.Checked != (e.Name != "unmapped") {
			t.Errorf("%s: checked = %v", e.Name, e.Checked)
		}
	}
	want := map[string]string{"bat": "GHSA-p24j-h477-76q3,RUSTSEC-2021-0106", "esbuild": "", "unmapped": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vulns = %v, want %v", got, want)
	}
}

func TestRunAuditCleanTable(t *testing.T) {
	auditSetup(t, nil)

	var runErr error
	out := captureStdout(t, func() { runErr = runAudit(cmdWithContext(), nil) })

	if runErr != nil {
		t.Fatalf("runAudit: %v", runErr)
	}
	for _, want := range []string{"VULNS", "none known", "? (no OSV mapping)"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the table, got:\n%s", want, out)
		}
	}
}
