package updatecheck

import (
	"runtime"
	"testing"
	"time"
)

func TestLoadMissingReturnsZero(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	st, err := Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if st.LatestVersion != "" || !st.LastChecked.IsZero() {
		t.Fatalf("expected zero State, got %+v", st)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	now := time.Now().UTC().Truncate(time.Second)
	want := State{LastChecked: now, LatestVersion: "1.2.3", LatestTag: "v1.2.3"}
	if err := want.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LatestVersion != want.LatestVersion || got.LatestTag != want.LatestTag ||
		!got.LastChecked.Equal(want.LastChecked) {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
	if got.Schema != schema {
		t.Fatalf("schema = %d, want %d", got.Schema, schema)
	}
}

func TestPathPerOS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", `C:\Users\me\AppData\Local`)
		got, err := Path()
		if err != nil {
			t.Fatalf("Path: %v", err)
		}
		want := `C:\Users\me\AppData\Local\paq\cache\update-check.json`
		if got != want {
			t.Fatalf("Path = %q, want %q", got, want)
		}
		return
	}

	t.Setenv("XDG_CACHE_HOME", "/tmp/xdgcache")
	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != "/tmp/xdgcache/paq/update-check.json" {
		t.Fatalf("Path = %q", got)
	}
}
