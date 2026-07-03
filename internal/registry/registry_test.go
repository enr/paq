package registry

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// setCacheHome points the cache dir at a temp directory for the test.
func setCacheHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", tmp)
	} else {
		t.Setenv("XDG_CACHE_HOME", tmp)
	}
	return tmp
}

func TestDir(t *testing.T) {
	tmp := setCacheHome(t)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, "paq", "registry")
	if runtime.GOOS == "windows" {
		want = filepath.Join(tmp, "paq", "cache", "registry")
	}
	if dir != want {
		t.Errorf("Dir() = %q, want %q", dir, want)
	}
}

func TestOpenAbsent(t *testing.T) {
	setCacheHome(t)
	fsys, meta, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if fsys != nil || meta != nil {
		t.Errorf("Open() on absent cache = (%v, %v), want (nil, nil)", fsys, meta)
	}
}

func TestOpenCorrupt(t *testing.T) {
	setCacheHome(t)
	dir, _ := Dir()

	// Snapshot dir without meta.json.
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(); err == nil {
		t.Error("Open() on snapshot without meta.json should fail")
	}

	// Unparsable meta.json.
	if err := os.WriteFile(filepath.Join(dir, metaFile), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(); err == nil {
		t.Error("Open() on corrupt meta.json should fail")
	}
}

// newStaging creates a staging dir containing a minimal registry snapshot.
func newStaging(t *testing.T, recipe string) string {
	t.Helper()
	staging, err := StagingDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "registry"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "registry", "tool.toml"), []byte(recipe), 0644); err != nil {
		t.Fatal(err)
	}
	return staging
}

func TestInstallFreshAndReplace(t *testing.T) {
	setCacheHome(t)

	// Fresh install.
	staging := newStaging(t, "[tool]\nbackend = \"github\"\n")
	meta := Meta{Tag: "v0.1.0", Version: "0.1.0", FetchedAt: time.Now(), SpecCount: 1}
	if err := Install(staging, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("staging dir should be gone after Install")
	}

	fsys, got, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Version != "0.1.0" || got.Schema != metaSchema {
		t.Errorf("meta after install = %+v", got)
	}
	if _, err := fs.ReadFile(fsys, "registry/tool.toml"); err != nil {
		t.Errorf("snapshot content not readable: %v", err)
	}

	// Replace with a newer snapshot.
	staging2 := newStaging(t, "[tool]\nbackend = \"url\"\n")
	if err := Install(staging2, Meta{Tag: "v0.2.0", Version: "0.2.0"}); err != nil {
		t.Fatal(err)
	}
	fsys, got, err = Open()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "0.2.0" {
		t.Errorf("meta.Version after replace = %q, want 0.2.0", got.Version)
	}
	data, err := fs.ReadFile(fsys, "registry/tool.toml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[tool]\nbackend = \"url\"\n" {
		t.Errorf("snapshot content not replaced: %q", data)
	}

	// No leftover .old or staging dirs.
	dir, _ := Dir()
	if _, err := os.Stat(dir + ".old"); !os.IsNotExist(err) {
		t.Error(".old backup dir left behind")
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	for _, e := range entries {
		if e.Name() != filepath.Base(dir) {
			t.Errorf("unexpected leftover in cache dir: %s", e.Name())
		}
	}
}

func TestInstallFailureKeepsPrevious(t *testing.T) {
	setCacheHome(t)

	staging := newStaging(t, "[tool]\nbackend = \"github\"\n")
	if err := Install(staging, Meta{Version: "0.1.0"}); err != nil {
		t.Fatal(err)
	}

	// A staging path that doesn't exist makes Install fail early.
	if err := Install(filepath.Join(t.TempDir(), "missing"), Meta{Version: "0.2.0"}); err == nil {
		t.Fatal("Install with missing staging dir should fail")
	}

	// The previous snapshot must still be in place.
	_, meta, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.Version != "0.1.0" {
		t.Errorf("previous snapshot lost after failed install, meta = %+v", meta)
	}
}
