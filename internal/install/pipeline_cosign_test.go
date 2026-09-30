package install

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/verify"
)

// newTestCosignKey generates a throwaway ECDSA P-256 key pair (cosign's
// default) and returns it with the PEM public key a recipe holds.
func newTestCosignKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(k.Public())
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// signCosign returns the base64 signature "cosign sign-blob --key" writes.
func signCosign(t *testing.T, k *ecdsa.PrivateKey, data []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, k, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return []byte(base64.StdEncoding.EncodeToString(sig))
}

// cosignFixture serves a tar.gz artifact, its checksum file and a cosign
// signature made by signer over the checksum file (or over the artifact when
// signArtifact is set). It counts the artifact requests.
func cosignFixture(t *testing.T, signer *ecdsa.PrivateKey, signArtifact bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	tgzData := makeFakeTarGz([]byte("fake-rg-binary"))
	checksumFile := []byte(fmt.Sprintf("%s  tool-1.0.0.tar.gz\n", sha256hex(tgzData)))
	signed := checksumFile
	if signArtifact {
		signed = tgzData
	}
	var artifactRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".sig"):
			w.Write(signCosign(t, signer, signed))
		case strings.HasSuffix(r.URL.Path, ".sigstore.json"):
			sum := sha256.Sum256(signed)
			fmt.Fprintf(w, `{"messageSignature":{"messageDigest":{"algorithm":"SHA2_256","digest":%q},"signature":%q}}`,
				base64.StdEncoding.EncodeToString(sum[:]), signCosign(t, signer, signed))
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			w.Write(checksumFile)
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			artifactRequests.Add(1)
			w.Write(tgzData)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &artifactRequests
}

func cosignConfig(t *testing.T, srvURL string, v config.VerifyConfig) (*config.Config, string) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "rg")
	return &config.Config{
		Specs: map[string]config.Spec{"tool": {
			Backend: "url",
			Source:  srvURL + "/tool-{{version}}.tar.gz",
			Archive: "tar.gz",
			Extract: "rg",
			Verify:  v,
		}},
		Apps: map[string]config.AppEntry{
			"tool": {Use: "tool", Version: "1.0.0", Dest: dest},
		},
	}, dest
}

// TestPipelineVerifiesCosignSignatureOfChecksum verifies the happy path of a
// cosign key-based signature over the checksum file.
func TestPipelineVerifiesCosignSignatureOfChecksum(t *testing.T) {
	isolateState(t)
	k, pub := newTestCosignKey(t)
	srv, _ := cosignFixture(t, k, false)
	cfg, dest := cosignConfig(t, srv.URL, config.VerifyConfig{
		SHA256Asset: "{{asset}}.sha256",
		Cosign:      config.CosignConfig{PublicKey: pub, Signature: "{{asset}}.sha256.sig"},
	})

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("dest not found: %v", err)
	}
	if !bytes.Equal(data, []byte("fake-rg-binary")) {
		t.Errorf("dest content = %q", data)
	}
}

// TestPipelineVerifiesCosignSignatureOfArtifact verifies that, with no
// checksum document, the signature is checked against the artifact itself.
func TestPipelineVerifiesCosignSignatureOfArtifact(t *testing.T) {
	isolateState(t)
	k, pub := newTestCosignKey(t)
	srv, _ := cosignFixture(t, k, true)
	cfg, _ := cosignConfig(t, srv.URL, config.VerifyConfig{
		Cosign: config.CosignConfig{PublicKey: pub, Signature: "{{asset}}.sig"},
	})

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
}

// TestPipelineVerifiesCosignKeyBundle verifies a key-based Sigstore bundle
// over the checksum file, checked in-process.
func TestPipelineVerifiesCosignKeyBundle(t *testing.T) {
	isolateState(t)
	k, pub := newTestCosignKey(t)
	srv, _ := cosignFixture(t, k, false)
	cfg, _ := cosignConfig(t, srv.URL, config.VerifyConfig{
		SHA256Asset: "{{asset}}.sha256",
		Cosign:      config.CosignConfig{PublicKey: pub, Bundle: "{{asset}}.sha256.sigstore.json"},
	})

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
}

// keylessConfig is a keyless [verify.cosign] block over the checksum file.
func keylessConfig() config.VerifyConfig {
	return config.VerifyConfig{
		SHA256Asset: "{{asset}}.sha256",
		Cosign: config.CosignConfig{
			Bundle:                "{{asset}}.sha256.sigstore.json",
			CertificateOIDCIssuer: "https://token.actions.githubusercontent.com",
			CertificateIdentity:   "https://github.com/owner/tool/.github/workflows/release.yml@refs/tags/v{{version}}",
		},
	}
}

// TestPipelineKeylessCosign verifies that a keyless bundle is handed to the
// external cosign: its exit 0 lets the install proceed, a non-zero exit fails
// it with ErrVerification before the artifact is downloaded.
func TestPipelineKeylessCosign(t *testing.T) {
	for name, code := range map[string]int{"valid": 0, "invalid": 1} {
		t.Run(name, func(t *testing.T) {
			isolateCosign(t)
			pathDir := t.TempDir()
			writeFakeCosign(t, filepath.Join(pathDir, "cosign"), "v3.1.3", code)
			t.Setenv("PATH", pathDir)
			k, _ := newTestCosignKey(t)
			srv, artifactRequests := cosignFixture(t, k, false)
			cfg, _ := cosignConfig(t, srv.URL, keylessConfig())

			err := Run(context.Background(), cfg, "tool", nil, nil)
			if code == 0 {
				if err != nil {
					t.Fatalf("install failed: %v", err)
				}
				return
			}
			if !errors.Is(err, verify.ErrVerification) || !strings.Contains(err.Error(), "fake cosign verdict") {
				t.Fatalf("error = %v, want ErrVerification with cosign's message", err)
			}
			if n := artifactRequests.Load(); n != 0 {
				t.Errorf("artifact endpoint was requested %d times, want 0", n)
			}
		})
	}
}

// TestPipelineCosignInstallRemovesPrivateCopy verifies that installing the
// cosign spec as a regular tool removes paq's private copy.
func TestPipelineCosignInstallRemovesPrivateCopy(t *testing.T) {
	isolateCosign(t)
	private, err := privateCosignPath()
	if err != nil {
		t.Fatal(err)
	}
	writeFakeCosign(t, private, "v3.1.3", 0)
	k, _ := newTestCosignKey(t)
	srv, _ := cosignFixture(t, k, false)
	cfg, _ := cosignConfig(t, srv.URL, config.VerifyConfig{SHA256Asset: "{{asset}}.sha256"})
	cfg.Specs["cosign"] = cfg.Specs["tool"]
	cfg.Apps["tool"] = config.AppEntry{Use: "cosign", Version: "1.0.0", Dest: cfg.Apps["tool"].Dest}

	if err := Run(context.Background(), cfg, "tool", nil, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if fileExists(private) {
		t.Error("private cosign copy still present after installing cosign")
	}
}

// TestPipelineRejectsBadCosignSignature verifies that a checksum file signed
// by another key fails with ErrVerification before the artifact is downloaded.
func TestPipelineRejectsBadCosignSignature(t *testing.T) {
	isolateState(t)
	_, pub := newTestCosignKey(t)
	other, _ := newTestCosignKey(t)
	srv, artifactRequests := cosignFixture(t, other, false)
	cfg, _ := cosignConfig(t, srv.URL, config.VerifyConfig{
		SHA256Asset: "{{asset}}.sha256",
		Cosign:      config.CosignConfig{PublicKey: pub, Signature: "{{asset}}.sha256.sig"},
	})

	err := Run(context.Background(), cfg, "tool", nil, nil)
	if !errors.Is(err, verify.ErrVerification) {
		t.Fatalf("error = %v, want ErrVerification", err)
	}
	if n := artifactRequests.Load(); n != 0 {
		t.Errorf("artifact endpoint was requested %d times, want 0 (signature must be checked before download)", n)
	}
}

// TestPipelineInvalidCosignConfigFails verifies that an incomplete or
// contradictory [verify.cosign] block is rejected before any network access.
func TestPipelineInvalidCosignConfigFails(t *testing.T) {
	isolateState(t)
	const issuer = "https://token.actions.githubusercontent.com"
	for name, tc := range map[string]struct {
		c    config.CosignConfig
		want string
	}{
		"only public_key":       {config.CosignConfig{PublicKey: "k"}, "requires signature or bundle"},
		"signature without key": {config.CosignConfig{Signature: "{{asset}}.sig"}, "public_key and signature"},
		"signature and bundle":  {config.CosignConfig{PublicKey: "k", Signature: "s", Bundle: "b"}, "mutually exclusive"},
		"key with identity":     {config.CosignConfig{PublicKey: "k", Bundle: "b", CertificateOIDCIssuer: issuer}, "excludes the certificate_*"},
		"keyless no issuer":     {config.CosignConfig{Bundle: "b", CertificateIdentity: "x"}, "certificate_oidc_issuer"},
		"keyless no identity":   {config.CosignConfig{Bundle: "b", CertificateOIDCIssuer: issuer}, "exactly one of"},
		"keyless two identities": {config.CosignConfig{Bundle: "b", CertificateOIDCIssuer: issuer,
			CertificateIdentity: "x", CertificateIdentityRegexp: "^x"}, "exactly one of"},
		"unanchored regexp": {config.CosignConfig{Bundle: "b", CertificateOIDCIssuer: issuer,
			CertificateIdentityRegexp: "https://github.com/owner/"}, "anchored"},
	} {
		cfg, _ := cosignConfig(t, "https://unreachable.invalid", config.VerifyConfig{Cosign: tc.c})
		err := Run(context.Background(), cfg, "tool", nil, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want mention of %q", name, err, tc.want)
		}
	}
}

// attestationFixture serves a tar.gz artifact and, on host api.github.com,
// the attestations API answering apiStatus/apiBody for the artifact's digest.
// It returns a client routing every host to it.
func attestationFixture(t *testing.T, apiStatus int, apiBody string) (*httptest.Server, *http.Client) {
	t.Helper()
	tgzData := makeFakeTarGz([]byte("fake-rg-binary"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.github.com" {
			want := "/repos/owner/tool/attestations/sha256:" + sha256hex(tgzData)
			if r.URL.Path != want {
				t.Errorf("attestations request %s, want %s", r.URL.Path, want)
			}
			w.WriteHeader(apiStatus)
			fmt.Fprint(w, apiBody)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			w.Write(tgzData)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &http.Client{Transport: &redirectTransport{base: srv.URL, inner: http.DefaultTransport}}
}

// TestPipelineGitHubAttestation verifies that an attestation is fetched for
// the artifact's digest and checked by cosign before anything is installed:
// a valid one lets the install proceed, a missing or invalid one fails it
// with ErrVerification and leaves the destination untouched.
func TestPipelineGitHubAttestation(t *testing.T) {
	for name, tc := range map[string]struct {
		apiStatus  int
		apiBody    string
		cosignExit int
		wantErr    string
	}{
		"valid":          {http.StatusOK, `{"attestations":[{"bundle":{"mediaType":"x"}}]}`, 0, ""},
		"no attestation": {http.StatusNotFound, `{"message":"Not Found"}`, 0, "no GitHub attestation"},
		"invalid":        {http.StatusOK, `{"attestations":[{"bundle":{"mediaType":"x"}}]}`, 1, "fake cosign verdict"},
	} {
		t.Run(name, func(t *testing.T) {
			isolateCosign(t)
			pathDir := t.TempDir()
			writeFakeCosign(t, filepath.Join(pathDir, "cosign"), "v3.1.3", tc.cosignExit)
			t.Setenv("PATH", pathDir)
			srv, client := attestationFixture(t, tc.apiStatus, tc.apiBody)
			cfg, dest := cosignConfig(t, srv.URL, config.VerifyConfig{
				GitHubAttestation: &config.GitHubAttestationConfig{Repo: "owner/tool"},
			})

			err := Run(context.Background(), cfg, "tool", nil, &Hooks{HTTPClient: client})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("install failed: %v", err)
				}
				return
			}
			if !errors.Is(err, verify.ErrVerification) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want ErrVerification mentioning %q", err, tc.wantErr)
			}
			if fileExists(dest) {
				t.Error("artifact was installed despite the failed attestation")
			}
		})
	}
}

// TestPipelineGitHubAttestationInvalidConfig verifies the checks made before
// any network access.
func TestPipelineGitHubAttestationInvalidConfig(t *testing.T) {
	isolateState(t)
	cfg, _ := cosignConfig(t, "https://unreachable.invalid", config.VerifyConfig{
		GitHubAttestation: &config.GitHubAttestationConfig{},
	})
	if err := Run(context.Background(), cfg, "tool", nil, nil); err == nil || !strings.Contains(err.Error(), "owner/name") {
		t.Errorf("no repo: error = %v, want mention of owner/name", err)
	}

	cfg, _ = cosignConfig(t, "https://unreachable.invalid", config.VerifyConfig{
		GitHubAttestation: &config.GitHubAttestationConfig{Repo: "sigstore/cosign"},
	})
	cfg.Specs["cosign"] = cfg.Specs["tool"]
	cfg.Apps["tool"] = config.AppEntry{Use: "cosign", Version: "1.0.0", Dest: cfg.Apps["tool"].Dest}
	if err := Run(context.Background(), cfg, "tool", nil, nil); err == nil || !strings.Contains(err.Error(), "would need itself") {
		t.Errorf("cosign with attestation: error = %v, want the recursion guard", err)
	}
}
