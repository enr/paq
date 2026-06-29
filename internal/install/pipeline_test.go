package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/version"
)

// makeFakeTarGz crea un .tar.gz in-memory con un singolo file "rg" contenente fakeContent.
func makeFakeTarGz(content []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "ripgrep-0.1.0-x86_64-unknown-linux-gnu/rg", Mode: 0755, Size: int64(len(content))})
	tw.Write(content)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func sha512hex(data []byte) string {
	h := sha512.Sum512(data)
	return hex.EncodeToString(h[:])
}

// makeFakeZip crea uno zip in-memory con un singolo file annidato sotto una
// directory di primo livello (come gli archivi maven), per testare strip_components.
func makeFakeZip(topDir, name string, content []byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(topDir + "/" + name)
	w.Write(content)
	zw.Close()
	return buf.Bytes()
}

// isolateState redirige lo state file di paq su una directory temporanea,
// così che i test che eseguono Run() non scrivano nello state reale dell'utente.
func isolateState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// TestPipelineSHA512URLBackend verifica l'installazione tramite backend "url"
// con verifica sha512 da file checksum "bare hash" (layout Apache Maven).
func TestPipelineSHA512URLBackend(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-mvn-binary")
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", fileContent)
	checksum := sha512hex(zipData) // bare hash, nessun filename

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(checksum + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify: config.VerifyConfig{
					SHA512Asset: "{{asset}}.sha512",
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {
				Use:     "maven",
				Version: "1.0.0",
				Dest:    dest,
			},
		},
	}

	if err := Run(context.Background(), cfg, "maven", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "mvn"))
	if err != nil {
		t.Fatalf("installed file not found: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("installed content = %q, want %q", data, fileContent)
	}
}

// TestPipelineOmittedVersionUsesDefault verifica che un'app SENZA version
// installi la default_version della spec (canale stabile, niente rete).
func TestPipelineOmittedVersionUsesDefault(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-mvn-binary")
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", fileContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Write(zipData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				DefaultVersion:  "1.0.0",
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Dest: dest}, // version omessa → default_version
		},
	}

	if err := Run(context.Background(), cfg, "maven", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "mvn"))
	if err != nil {
		t.Fatalf("installed file not found: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("installed content = %q, want %q", data, fileContent)
	}
}

// TestPipelineOmittedDestUsesDefaults verifica che un'app SENZA dest derivi la
// destinazione dalle directory base configurate in [defaults].
func TestPipelineOmittedDestUsesDefaults(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-mvn-binary")
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", fileContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Write(zipData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	optDir := t.TempDir()
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				DefaultVersion:  "1.0.0",
			},
		},
		Defaults: config.Defaults{Opt: optDir},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven"}, // né version né dest
		},
	}

	if err := Run(context.Background(), cfg, "maven", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	// dest derivato = <opt>/maven (spec directory-style).
	data, err := os.ReadFile(filepath.Join(optDir, "maven", "bin", "mvn"))
	if err != nil {
		t.Fatalf("installed file not found at default dest: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("installed content = %q, want %q", data, fileContent)
	}
}

// TestPipelineLatestNoStrategyErrors verifica che version="latest" su una
// spec senza strategia reale (backend "url") fallisca, senza ricadere su
// una default_version.
func TestPipelineLatestNoStrategyErrors(t *testing.T) {
	isolateState(t)
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"tool": {
				Backend: "url",
				Source:  "https://example.com/{{version}}.zip",
				Archive: "zip",
			},
		},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "latest", Dest: filepath.Join(t.TempDir(), "tool")},
		},
	}

	err := Run(context.Background(), cfg, "tool", nil, nil)
	if !errors.Is(err, version.ErrLatestNotImplemented) {
		t.Fatalf("expected ErrLatestNotImplemented, got %v", err)
	}
}

// TestPipelineSHA512Mismatch verifica che un checksum sha512 errato faccia
// fallire l'installazione senza creare la destinazione.
func TestPipelineSHA512Mismatch(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", []byte("payload"))
	wrong := strings.Repeat("0", 128)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(wrong + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify:          config.VerifyConfig{SHA512Asset: "{{asset}}.sha512"},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	if err := Run(context.Background(), cfg, "maven", nil, nil); err == nil {
		t.Error("expected error for sha512 mismatch, got nil")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("dest should not exist after failed install")
	}
}

// TestPipelineWarnsWhenNoVerify verifica che la pipeline emetta un warning
// (hook OnWarn) quando la spec non configura alcuna verifica.
func TestPipelineWarnsWhenNoVerify(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", []byte("payload"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Write(zipData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				// Nessun blocco Verify.
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	var warnings []string
	hooks := &Hooks{OnWarn: func(msg string) { warnings = append(warnings, msg) }}

	if err := Run(context.Background(), cfg, "maven", nil, hooks); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning when no verification is configured, got none")
	}
	if !strings.Contains(warnings[0], "no verification") {
		t.Errorf("unexpected warning message: %q", warnings[0])
	}
}

// TestPipelineNoWarnWhenVerifyConfigured verifica che il warning NON venga
// emesso quando la verifica è configurata.
func TestPipelineNoWarnWhenVerifyConfigured(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", []byte("payload"))
	checksum := sha512hex(zipData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(checksum + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify:          config.VerifyConfig{SHA512Asset: "{{asset}}.sha512"},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	var warnings []string
	hooks := &Hooks{OnWarn: func(msg string) { warnings = append(warnings, msg) }}

	if err := Run(context.Background(), cfg, "maven", nil, hooks); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warning when verification is configured, got: %v", warnings)
	}
}

func TestPipelineInstallFile(t *testing.T) {
	isolateState(t)
	binaryContent := []byte("fake-rg-binary")
	tgzData := makeFakeTarGz(binaryContent)
	assetName := "ripgrep-0.1.0-x86_64-unknown-linux-gnu.tar.gz"
	checksum := sha256hex(tgzData)
	checksumFile := fmt.Sprintf("%s  %s\n", checksum, assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "releases/latest"):
			json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.1.0"})
		case strings.Contains(r.URL.Path, "releases/tags"):
			json.NewEncoder(w).Encode(map[string]any{
				"assets": []map[string]string{
					{
						"name": assetName,
						"url":  "http://" + r.Host + "/download/" + assetName,
					},
					{
						"name": assetName + ".sha256",
						"url":  "http://" + r.Host + "/download/" + assetName + ".sha256",
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			w.Write([]byte(checksumFile))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			w.Write(tgzData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rg")

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"ripgrep": {
				Backend: "github",
				Repo:    "test/ripgrep",
				Asset:   "ripgrep-{{version}}-x86_64-unknown-linux-gnu.tar.gz",
				Archive: "tar.gz",
				Extract: "rg",
				Chmod:   "0755",
				Verify: config.VerifyConfig{
					SHA256Asset: "{{asset}}.sha256",
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"rg": {
				Use:     "ripgrep",
				Version: "latest",
				Dest:    dest,
			},
		},
	}

	// Patch: l'API GitHub deve puntare al server di test
	// Per semplicità, modifichiamo la spec usando backend "url"
	// e testiamo la pipeline con mock della GitHub API via transport.
	// Usiamo un approccio più diretto: monkey-patch dell'HTTP client.
	// Il test usa un custom transport che redirige verso il server di test.
	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{base: srv.URL, inner: origTransport}
	defer func() { http.DefaultTransport = origTransport }()

	err := Run(context.Background(), cfg, "rg", nil, nil)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Verifica che il file sia stato installato
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("dest not found: %v", err)
	}
	if !bytes.Equal(data, binaryContent) {
		t.Errorf("dest content = %q, want %q", data, binaryContent)
	}
}

func TestPipelineChecksumMismatch(t *testing.T) {
	isolateState(t)
	binaryContent := []byte("fake-rg-binary")
	tgzData := makeFakeTarGz(binaryContent)
	assetName := "ripgrep-0.1.0-x86_64-unknown-linux-gnu.tar.gz"
	// Checksum volutamente sbagliato
	wrongChecksum := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	checksumFile := fmt.Sprintf("%s  %s\n", wrongChecksum, assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "releases/latest"):
			json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.1.0"})
		case strings.Contains(r.URL.Path, "releases/tags"):
			json.NewEncoder(w).Encode(map[string]any{
				"assets": []map[string]string{
					{
						"name": assetName,
						"url":  "http://" + r.Host + "/download/" + assetName,
					},
					{
						"name": assetName + ".sha256",
						"url":  "http://" + r.Host + "/download/" + assetName + ".sha256",
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			w.Write([]byte(checksumFile))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			w.Write(tgzData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rg")

	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"ripgrep": {
				Backend: "github",
				Repo:    "test/ripgrep",
				Asset:   "ripgrep-{{version}}-x86_64-unknown-linux-gnu.tar.gz",
				Archive: "tar.gz",
				Extract: "rg",
				Verify: config.VerifyConfig{
					SHA256Asset: "{{asset}}.sha256",
				},
			},
		},
		Apps: map[string]config.AppEntry{
			"rg": {
				Use:     "ripgrep",
				Version: "latest",
				Dest:    dest,
			},
		},
	}

	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{base: srv.URL, inner: origTransport}
	defer func() { http.DefaultTransport = origTransport }()

	err := Run(context.Background(), cfg, "rg", nil, nil)
	if err == nil {
		t.Error("expected error for checksum mismatch, got nil")
	}

	// dest NON deve essere stato creato
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("dest should not exist after failed install")
	}
}

// TestPipelineErrorShownOnceAndMarked verifica che, in caso di errore, l'hook
// OnFail venga invocato una sola volta e l'errore ritornato sia marcato come
// "già mostrato" (così il chiamante non lo ristampa).
func TestPipelineErrorShownOnceAndMarked(t *testing.T) {
	isolateState(t)
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", []byte("payload"))
	wrong := strings.Repeat("0", 128)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(wrong + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify:          config.VerifyConfig{SHA512Asset: "{{asset}}.sha512"},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	var failCount int
	var failErr error
	hooks := &Hooks{OnFail: func(err error) { failCount++; failErr = err }}

	err := Run(context.Background(), cfg, "maven", nil, hooks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if failCount != 1 {
		t.Errorf("OnFail called %d times, want 1", failCount)
	}
	if failErr == nil || !strings.Contains(failErr.Error(), "sha512 mismatch") {
		t.Errorf("OnFail received unexpected error: %v", failErr)
	}
	if !ErrAlreadyShown(err) {
		t.Error("returned error should be marked as already shown")
	}
}

// TestPipelineDebugHook verifica che la pipeline emetta tracce di debug.
func TestPipelineDebugHook(t *testing.T) {
	isolateState(t)
	fileContent := []byte("fake-mvn")
	zipData := makeFakeZip("apache-maven-1.0.0", "bin/mvn", fileContent)
	checksum := sha512hex(zipData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.sha512"):
			w.Write([]byte(checksum + "\n"))
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "maven")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"maven": {
				Backend:         "url",
				Source:          srv.URL + "/maven-{{version_major}}/{{version}}/binaries/apache-maven-{{version}}-bin.zip",
				Archive:         "zip",
				StripComponents: 1,
				Verify:          config.VerifyConfig{SHA512Asset: "{{asset}}.sha512"},
			},
		},
		Apps: map[string]config.AppEntry{
			"maven": {Use: "maven", Version: "1.0.0", Dest: dest},
		},
	}

	var debugLines []string
	hooks := &Hooks{OnDebug: func(msg string) { debugLines = append(debugLines, msg) }}

	if err := Run(context.Background(), cfg, "maven", nil, hooks); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(debugLines) == 0 {
		t.Fatal("expected debug output, got none")
	}
	joined := strings.Join(debugLines, "\n")
	for _, want := range []string{"asset name", "sha512 checksum URL", "artifact sha256"} {
		if !strings.Contains(joined, want) {
			t.Errorf("debug output missing %q; got:\n%s", want, joined)
		}
	}
}

type redirectTransport struct {
	base  string
	inner http.RoundTripper
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = strings.TrimPrefix(rt.base, "http://")
	return rt.inner.RoundTrip(req2)
}
