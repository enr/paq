package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/registry"
	"github.com/spf13/cobra"

	minisign "github.com/jedisct1/go-minisign"
)

// signer holds a test-only minisign keypair.
type signer struct {
	sk     minisign.PrivateKey
	pubB64 string
}

func newSigner(t *testing.T) signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var sk minisign.PrivateKey
	sk.SignatureAlgorithm = [2]byte{'E', 'd'}
	copy(sk.SecretKey[:], priv)

	pk := sk.PublicKey()
	buf := make([]byte, 0, 42)
	buf = append(buf, pk.SignatureAlgorithm[:]...)
	buf = append(buf, pk.KeyId[:]...)
	buf = append(buf, pk.PublicKey[:]...)
	return signer{sk: sk, pubB64: base64.StdEncoding.EncodeToString(buf)}
}

// sign returns the encoded .minisig for data.
func (s signer) sign(t *testing.T, data []byte) []byte {
	t.Helper()
	sig, err := s.sk.Sign(data, minisign.SignOptions{Hashed: true})
	if err != nil {
		t.Fatal(err)
	}
	return sig.Encode()
}

// makeTarGz builds a gzip'd tar with the given files (path → content).
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Line(data []byte, name string) []byte {
	sum := sha256.Sum256(data)
	return []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), name))
}

// fixture is a served registry release (tarball + checksum + signature).
type fixture struct {
	tar  []byte
	sums []byte
	sig  []byte
}

// serve starts an HTTPS server exposing the three registry assets and points
// registryUpdateClient at it; returns the base tarball URL.
func serve(t *testing.T, f fixture) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/registry.tar.gz", func(w http.ResponseWriter, r *http.Request) { w.Write(f.tar) })
	mux.HandleFunc("/registry.tar.gz.sha256", func(w http.ResponseWriter, r *http.Request) { w.Write(f.sums) })
	mux.HandleFunc("/registry.tar.gz.sha256.minisig", func(w http.ResponseWriter, r *http.Request) { w.Write(f.sig) })
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	prev := registryUpdateClient
	registryUpdateClient = func() *http.Client { return ts.Client() }
	t.Cleanup(func() { registryUpdateClient = prev })

	return ts.URL + "/registry.tar.gz"
}

// setupEnv points config and cache dirs at temp dirs and writes a [registry]
// manifest for the custom-URL path.
func setupEnv(t *testing.T, url, pubKey string) {
	t.Helper()
	cfgHome := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", cfgHome)
		t.Setenv("LOCALAPPDATA", cacheHome)
	}
	dir := filepath.Join(cfgHome, "paq")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("[registry]\nurl = %q\npublic_key = %q\n", url, pubKey)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
}

func runUpdate(t *testing.T, force bool) error {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().BoolP("force", "f", false, "")
	if force {
		if err := cmd.Flags().Set("force", "true"); err != nil {
			t.Fatal(err)
		}
	}
	return runRegistryUpdate(cmd, nil)
}

// validFixture builds a signed, well-formed registry release at the given version.
func validFixture(t *testing.T, s signer, version, recipe string) fixture {
	t.Helper()
	tarball := makeTarGz(t, map[string]string{
		"registry/tool.toml": recipe,
		"registry/VERSION":   version + "\n",
	})
	sums := sha256Line(tarball, "registry.tar.gz")
	return fixture{tar: tarball, sums: sums, sig: s.sign(t, sums)}
}

func TestRegistryUpdateHappyPath(t *testing.T) {
	s := newSigner(t)
	f := validFixture(t, s, "1.2.0", "[newtool]\nbackend = \"github\"\nrepo = \"owner/newtool\"\n")
	url := serve(t, f)
	setupEnv(t, url, s.pubB64)

	if err := runUpdate(t, false); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	_, meta, err := registry.Open()
	if err != nil || meta == nil {
		t.Fatalf("snapshot not installed: meta=%v err=%v", meta, err)
	}
	if meta.Version != "1.2.0" || meta.SpecCount != 1 {
		t.Errorf("meta = %+v, want version 1.2.0, 1 spec", meta)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	nt, ok := cfg.Specs["newtool"]
	if !ok {
		t.Fatal("newtool not visible after update")
	}
	if nt.Origin != config.OriginRegistry {
		t.Errorf("newtool.Origin = %q, want %q", nt.Origin, config.OriginRegistry)
	}
}

func TestRegistryUpdateBadSignature(t *testing.T) {
	s := newSigner(t)
	other := newSigner(t)
	f := validFixture(t, s, "1.0.0", "[t]\nbackend = \"github\"\n")
	f.sig = other.sign(t, f.sums) // signed by a different key
	url := serve(t, f)
	setupEnv(t, url, s.pubB64)

	err := runUpdate(t, false)
	if err == nil {
		t.Fatal("update should fail on bad signature")
	}
	if exitCodeFor(err) != exitVerify {
		t.Errorf("exit code = %d, want %d (verify); err = %v", exitCodeFor(err), exitVerify, err)
	}
	if _, meta, _ := registry.Open(); meta != nil {
		t.Error("no snapshot should be installed after a failed update")
	}
}

func TestRegistryUpdateTamperedTarball(t *testing.T) {
	s := newSigner(t)
	f := validFixture(t, s, "1.0.0", "[t]\nbackend = \"github\"\n")
	// Keep the signed checksum but serve a different tarball.
	f.tar = makeTarGz(t, map[string]string{
		"registry/tool.toml": "[t]\nbackend = \"url\"\n",
		"registry/VERSION":   "1.0.0\n",
	})
	url := serve(t, f)
	setupEnv(t, url, s.pubB64)

	err := runUpdate(t, false)
	if err == nil {
		t.Fatal("update should fail on tampered tarball")
	}
	if exitCodeFor(err) != exitVerify {
		t.Errorf("exit code = %d, want %d (verify); err = %v", exitCodeFor(err), exitVerify, err)
	}
}

func TestRegistryUpdateRejectsHTTP(t *testing.T) {
	setupEnv(t, "http://example.com/registry.tar.gz", "somekey")
	if err := runUpdate(t, false); err == nil {
		t.Fatal("update should reject a non-https custom url")
	}
}

func TestRegistryUpdateRequiresPublicKey(t *testing.T) {
	setupEnv(t, "https://example.com/registry.tar.gz", "")
	if err := runUpdate(t, false); err == nil {
		t.Fatal("update should require public_key for a custom url")
	}
}

func TestRegistryUpdateDowngrade(t *testing.T) {
	s := newSigner(t)

	// Install 2.0.0.
	url := serve(t, validFixture(t, s, "2.0.0", "[t]\nbackend = \"github\"\n"))
	setupEnv(t, url, s.pubB64)
	if err := runUpdate(t, false); err != nil {
		t.Fatalf("initial install failed: %v", err)
	}

	// Serving 1.0.0 on the same URL: refused without --force.
	oldF := validFixture(t, s, "1.0.0", "[t]\nbackend = \"github\"\n")
	// Re-point the client at a new server serving the old fixture.
	serveOver(t, oldF, url)
	if err := runUpdate(t, false); err == nil {
		t.Fatal("downgrade should be refused without --force")
	}
	if _, meta, _ := registry.Open(); meta == nil || meta.Version != "2.0.0" {
		t.Errorf("snapshot changed after refused downgrade: %+v", meta)
	}

	// With --force the downgrade goes through.
	if err := runUpdate(t, true); err != nil {
		t.Fatalf("forced downgrade failed: %v", err)
	}
	if _, meta, _ := registry.Open(); meta == nil || meta.Version != "1.0.0" {
		t.Errorf("forced downgrade not applied: %+v", meta)
	}
}

func TestRegistryUpdateAlreadyUpToDate(t *testing.T) {
	s := newSigner(t)
	url := serve(t, validFixture(t, s, "1.0.0", "[t]\nbackend = \"github\"\n"))
	setupEnv(t, url, s.pubB64)
	if err := runUpdate(t, false); err != nil {
		t.Fatal(err)
	}
	// Same version again: no error, no change.
	if err := runUpdate(t, false); err != nil {
		t.Fatalf("re-update at same version should be a no-op: %v", err)
	}
	if _, meta, _ := registry.Open(); meta == nil || meta.Version != "1.0.0" {
		t.Errorf("meta = %+v, want 1.0.0", meta)
	}
}

func TestRegistryUpdateOversize(t *testing.T) {
	prev := registryMaxBytes
	registryMaxBytes = 16
	t.Cleanup(func() { registryMaxBytes = prev })

	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "paq-download-*"))

	s := newSigner(t)
	url := serve(t, validFixture(t, s, "1.0.0", "[t]\nbackend = \"github\"\n"))
	setupEnv(t, url, s.pubB64)
	err := runUpdate(t, false)
	if err == nil {
		t.Fatal("update should reject an oversized archive")
	}
	if !strings.Contains(err.Error(), "16") {
		t.Errorf("error = %v, want it to mention the byte limit (16)", err)
	}

	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "paq-download-*"))
	if len(after) > len(before) {
		t.Errorf("leftover paq-download temp files: before=%d after=%d", len(before), len(after))
	}
}

func TestRegistryUpdateNoRecipes(t *testing.T) {
	s := newSigner(t)
	tarball := makeTarGz(t, map[string]string{
		"registry/VERSION":        "1.0.0\n",
		"registry/templates.toml": "[templates]\nx = \"y\"\n",
	})
	sums := sha256Line(tarball, "registry.tar.gz")
	url := serve(t, fixture{tar: tarball, sums: sums, sig: s.sign(t, sums)})
	setupEnv(t, url, s.pubB64)
	if err := runUpdate(t, false); err == nil {
		t.Fatal("update should reject an archive with no recipes")
	}
}

func TestRegistryUpdateNoVersion(t *testing.T) {
	s := newSigner(t)
	tarball := makeTarGz(t, map[string]string{
		"registry/tool.toml": "[t]\nbackend = \"github\"\n",
	})
	sums := sha256Line(tarball, "registry.tar.gz")
	url := serve(t, fixture{tar: tarball, sums: sums, sig: s.sign(t, sums)})
	setupEnv(t, url, s.pubB64)
	if err := runUpdate(t, false); err == nil {
		t.Fatal("update should reject an archive without VERSION")
	}
}

// serveOver starts a new HTTPS server for f and repoints registryUpdateClient,
// asserting the new base URL matches wantURL (so the manifest keeps working).
func serveOver(t *testing.T, f fixture, wantURL string) {
	t.Helper()
	// httptest picks a random port, so we cannot reuse wantURL's host. Instead
	// rewrite the manifest to the new server's URL.
	newURL := serve(t, f)
	if newURL == wantURL {
		return
	}
	// Update the manifest to point at the new server.
	cfgHome := os.Getenv("XDG_CONFIG_HOME")
	s := readManifestSigner(t)
	manifest := fmt.Sprintf("[registry]\nurl = %q\npublic_key = %q\n", newURL, s)
	if err := os.WriteFile(filepath.Join(cfgHome, "paq", "config.toml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
}

// readManifestSigner reads the public_key from the current manifest.
func readManifestSigner(t *testing.T) string {
	t.Helper()
	cfg, err := config.LoadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Registry.PublicKey
}
