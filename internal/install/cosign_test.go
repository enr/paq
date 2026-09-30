package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/verify"
)

// isolateCosign gives the test its own state, cache and an empty PATH, so no
// real cosign on the machine is found.
func isolateCosign(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake cosign is a shell script")
	}
	isolateState(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
}

// writeFakeCosign writes a shell script standing in for cosign at path: it
// reports gitVersion ver and exits verifyCode on verify-blob.
func writeFakeCosign(t *testing.T, path, ver string, verifyCode int) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "version" ]; then echo '{"gitVersion": "%s"}'; exit 0; fi
echo 'fake cosign verdict' >&2
exit %d
`, ver, verifyCode)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

func noWarn(string) {}

func TestFindCosignOrder(t *testing.T) {
	isolateCosign(t)
	cfg := &config.Config{}

	if _, ok := FindCosign(cfg, noWarn); ok {
		t.Fatal("found a cosign in an empty environment")
	}

	private, err := privateCosignPath()
	if err != nil {
		t.Fatal(err)
	}
	writeFakeCosign(t, private, "v3.1.3", 0)
	assertOrigin(t, cfg, CosignFromPrivate, private)

	recorded := filepath.Join(t.TempDir(), "cosign")
	writeFakeCosign(t, recorded, "v3.1.3", 0)
	if err := state.Update(func(st *state.State) error {
		st.Record(state.InstalledApp{Name: "cosign", Version: "3.1.3", Kind: "binaries",
			Dest: filepath.Dir(recorded), Files: []string{recorded}, InstalledAt: time.Now()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertOrigin(t, cfg, CosignFromState, recorded)

	pathDir := t.TempDir()
	writeFakeCosign(t, filepath.Join(pathDir, "cosign"), "v3.0.1", 0)
	t.Setenv("PATH", pathDir)
	assertOrigin(t, cfg, CosignFromPath, filepath.Join(pathDir, "cosign"))

	override := filepath.Join(t.TempDir(), "my-cosign")
	writeFakeCosign(t, override, "v3.2.0", 0)
	cfg.Defaults.Cosign = override
	assertOrigin(t, cfg, CosignFromDefaults, override)
}

// TestFindCosignSkipsOldVersion verifies that a cosign older than the minimum
// is skipped, with a warning, in favor of the next candidate.
func TestFindCosignSkipsOldVersion(t *testing.T) {
	isolateCosign(t)
	pathDir := t.TempDir()
	writeFakeCosign(t, filepath.Join(pathDir, "cosign"), "v2.5.0", 0)
	t.Setenv("PATH", pathDir)
	private, _ := privateCosignPath()
	writeFakeCosign(t, private, "v3.1.3", 0)

	var warnings []string
	c, ok := FindCosign(&config.Config{}, func(m string) { warnings = append(warnings, m) })
	if !ok || c.Origin != CosignFromPrivate {
		t.Fatalf("got %+v, %v; want the private copy", c, ok)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "older than") {
		t.Errorf("warnings = %v, want one about the old version", warnings)
	}
}

func assertOrigin(t *testing.T, cfg *config.Config, origin, path string) {
	t.Helper()
	c, ok := FindCosign(cfg, noWarn)
	if !ok || c.Origin != origin || c.Path != path {
		t.Errorf("FindCosign = %+v, %v; want origin %q path %q", c, ok, origin, path)
	}
}

// servePinnedCosign serves body as the pinned cosign release binary for this
// platform, pins its hash (or a wrong one), and counts the downloads.
func servePinnedCosign(t *testing.T, body []byte, pinMatches bool) *atomic.Int32 {
	t.Helper()
	key := runtime.GOOS + "/" + runtime.GOARCH
	sum := sha256.Sum256(body)
	pin := hex.EncodeToString(sum[:])
	if !pinMatches {
		pin = strings.Repeat("0", 64)
	}
	oldPin, hadPin := cosignPinSHA256[key]
	cosignPinSHA256[key] = pin
	var downloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/%s/cosign-%s-%s", cosignPinVersion, runtime.GOOS, runtime.GOARCH) {
			http.NotFound(w, r)
			return
		}
		downloads.Add(1)
		w.Write(body)
	}))
	oldBase := cosignReleaseBase
	cosignReleaseBase = srv.URL
	t.Cleanup(func() {
		srv.Close()
		cosignReleaseBase = oldBase
		if hadPin {
			cosignPinSHA256[key] = oldPin
		} else {
			delete(cosignPinSHA256, key)
		}
	})
	return &downloads
}

func TestEnsureCosignInstallsPrivateCopy(t *testing.T) {
	isolateCosign(t)
	body := []byte("#!/bin/sh\necho '{\"gitVersion\": \"v3.1.3\"}'\n")
	downloads := servePinnedCosign(t, body, true)

	c, err := EnsureCosign(context.Background(), &config.Config{}, http.DefaultClient, nil, noWarn, noWarn)
	if err != nil {
		t.Fatalf("EnsureCosign: %v", err)
	}
	if c.Origin != CosignFromPrivate {
		t.Errorf("origin = %q, want private copy", c.Origin)
	}
	info, err := os.Stat(c.Path)
	if err != nil {
		t.Fatalf("private copy not on disk: %v", err)
	}
	if info.Mode().Perm()&0100 == 0 {
		t.Errorf("private copy mode = %v, want executable", info.Mode())
	}

	// A second call reuses the copy instead of downloading it again.
	if _, err := EnsureCosign(context.Background(), &config.Config{}, http.DefaultClient, nil, noWarn, noWarn); err != nil {
		t.Fatalf("second EnsureCosign: %v", err)
	}
	if n := downloads.Load(); n != 1 {
		t.Errorf("downloads = %d, want 1", n)
	}
}

// TestEnsureCosignRejectsWrongHash verifies that a download not matching the
// pinned hash is a verification failure and never lands in the cache.
func TestEnsureCosignRejectsWrongHash(t *testing.T) {
	isolateCosign(t)
	servePinnedCosign(t, []byte("tampered"), false)

	_, err := EnsureCosign(context.Background(), &config.Config{}, http.DefaultClient, nil, noWarn, noWarn)
	if !errors.Is(err, verify.ErrVerification) {
		t.Fatalf("error = %v, want ErrVerification", err)
	}
	if p, _ := privateCosignPath(); fileExists(p) {
		t.Error("tampered cosign was installed into the cache")
	}
}

func TestEnsureCosignMissingCallback(t *testing.T) {
	isolateCosign(t)
	downloads := servePinnedCosign(t, []byte("#!/bin/sh\n"), true)
	abort := errors.New("aborted")

	// An error aborts without downloading.
	_, err := EnsureCosign(context.Background(), &config.Config{}, http.DefaultClient,
		func(context.Context) (bool, error) { return false, abort }, noWarn, noWarn)
	if !errors.Is(err, abort) {
		t.Errorf("error = %v, want the callback's error", err)
	}

	// "User installed" makes EnsureCosign look again, and use what it finds.
	pathDir := t.TempDir()
	c, err := EnsureCosign(context.Background(), &config.Config{}, http.DefaultClient,
		func(context.Context) (bool, error) {
			writeFakeCosign(t, filepath.Join(pathDir, "cosign"), "v3.1.3", 0)
			t.Setenv("PATH", pathDir)
			return true, nil
		}, noWarn, noWarn)
	if err != nil || c.Origin != CosignFromPath {
		t.Errorf("got %+v, %v; want the user-installed cosign", c, err)
	}
	if n := downloads.Load(); n != 0 {
		t.Errorf("downloads = %d, want 0", n)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
