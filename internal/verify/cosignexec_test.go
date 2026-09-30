package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/enr/paq/internal/backend"
	"github.com/enr/paq/internal/download"
)

// fakeCosign writes a shell script standing in for cosign: it records its
// arguments in a file and exits with code, printing stderrMsg. Returns the
// script path and the path of the recorded arguments.
func fakeCosign(t *testing.T, code int, stderrMsg string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake cosign is a shell script")
	}
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsPath + "\necho '" + stderrMsg + "' >&2\nexit " + strconv.Itoa(code) + "\n"
	path := filepath.Join(dir, "cosign")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake cosign: %v", err)
	}
	return path, argsPath
}

func TestCheckCosignBundleArgs(t *testing.T) {
	for name, tc := range map[string]struct {
		id   CosignIdentity
		want string
	}{
		"exact identity": {
			id:   CosignIdentity{Issuer: "https://issuer", Identity: "https://id"},
			want: "verify-blob\n--bundle\nb.json\n--certificate-oidc-issuer\nhttps://issuer\n--certificate-identity\nhttps://id\nfile.txt\n",
		},
		"identity regexp": {
			id:   CosignIdentity{Issuer: "https://issuer", IdentityRegexp: "^https://id/"},
			want: "verify-blob\n--bundle\nb.json\n--certificate-oidc-issuer\nhttps://issuer\n--certificate-identity-regexp\n^https://id/\nfile.txt\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			cosign, argsPath := fakeCosign(t, 0, "Verified OK")
			if err := CheckCosignBundle(context.Background(), cosign, "file.txt", "b.json", tc.id); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatalf("read args: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("args =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestCheckCosignBundleFailure verifies that a non-zero cosign exit is a
// verification failure carrying cosign's message.
func TestCheckCosignBundleFailure(t *testing.T) {
	cosign, _ := fakeCosign(t, 1, "Error: none of the expected identities matched")
	err := CheckCosignBundle(context.Background(), cosign, "file.txt", "b.json", CosignIdentity{Issuer: "i", Identity: "x"})
	if !errors.Is(err, ErrVerification) {
		t.Fatalf("error = %v, want ErrVerification", err)
	}
	if !strings.Contains(err.Error(), "none of the expected identities matched") {
		t.Errorf("error = %q, want cosign's message", err)
	}
}

// TestCheckCosignBundleMissingBinary verifies that a cosign that cannot be
// started is a "could not check" error, not a verification verdict.
func TestCheckCosignBundleMissingBinary(t *testing.T) {
	err := CheckCosignBundle(context.Background(), filepath.Join(t.TempDir(), "nope"), "file.txt", "b.json", CosignIdentity{Issuer: "i", Identity: "x"})
	if err == nil || errors.Is(err, ErrVerification) {
		t.Errorf("error = %v, want a non-verification error", err)
	}
}

// TestCheckCosignBundleRealGoreleaser runs the real cosign against a real
// keyless bundle (goreleaser v2.18.2 checksums.txt). It needs a cosign binary
// and network access (cosign fetches its trust root), so it only runs when
// PAQ_E2E_COSIGN points at a cosign binary.
func TestCheckCosignBundleRealGoreleaser(t *testing.T) {
	cosign := os.Getenv("PAQ_E2E_COSIGN")
	if cosign == "" {
		t.Skip("PAQ_E2E_COSIGN not set")
	}
	if _, err := exec.LookPath(cosign); err != nil {
		t.Fatalf("PAQ_E2E_COSIGN: %v", err)
	}
	file := filepath.Join("testdata", "goreleaser-v2.18.2-checksums.txt")
	bundle := file + ".sigstore.json"
	ok := CosignIdentity{
		Issuer:   "https://token.actions.githubusercontent.com",
		Identity: "https://github.com/goreleaser/goreleaser/.github/workflows/release.yml@refs/tags/v2.18.2",
	}
	if err := CheckCosignBundle(context.Background(), cosign, file, bundle, ok); err != nil {
		t.Errorf("expected valid bundle, got error: %v", err)
	}
	wrong := ok
	wrong.Identity = "https://github.com/someone/else/.github/workflows/release.yml@refs/tags/v2.18.2"
	if err := CheckCosignBundle(context.Background(), cosign, file, bundle, wrong); !errors.Is(err, ErrVerification) {
		t.Errorf("wrong identity: error = %v, want ErrVerification", err)
	}
}

// fakeAttestCosign writes a fake cosign that records its arguments and
// accepts only a bundle whose content is "good".
func fakeAttestCosign(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake cosign is a shell script")
	}
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := `#!/bin/sh
printf '%s\n' "$@" > ` + argsPath + `
while [ $# -gt 0 ]; do
  if [ "$1" = "--bundle" ]; then bundle="$2"; fi
  shift
done
if [ "$(cat "$bundle")" = "good" ]; then exit 0; fi
echo 'Error: bad attestation' >&2
exit 1
`
	path := filepath.Join(dir, "cosign")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake cosign: %v", err)
	}
	return path, argsPath
}

func TestCheckGitHubAttestationsArgs(t *testing.T) {
	for name, tc := range map[string]struct {
		policy       AttestationPolicy
		wantIdentity string
	}{
		"repo":            {AttestationPolicy{Repo: "cli/cli"}, `^https://github\.com/cli/cli/`},
		"signer workflow": {AttestationPolicy{Repo: "cli/cli", SignerWorkflow: "cli/cli/.github/workflows/deployment.yml"}, `^https://github\.com/cli/cli/\.github/workflows/deployment\.yml@`},
	} {
		t.Run(name, func(t *testing.T) {
			cosign, argsPath := fakeAttestCosign(t)
			if err := CheckGitHubAttestations(context.Background(), cosign, "gh.tar.gz", [][]byte{[]byte("good")}, tc.policy); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSpace(string(got)), "\n")
			want := []string{"verify-blob-attestation", "--type", "https://slsa.dev/provenance/v1",
				"--certificate-oidc-issuer", GitHubActionsIssuer,
				"--certificate-identity-regexp", tc.wantIdentity,
				"--certificate-github-workflow-repository", "cli/cli", "--bundle"}
			if strings.Join(args[:len(want)], " ") != strings.Join(want, " ") || args[len(args)-1] != "gh.tar.gz" {
				t.Errorf("args = %q, want prefix %q and artifact last", args, want)
			}
		})
	}
}

// TestCheckGitHubAttestationsAnyValid verifies that one valid attestation
// among several is enough, and that none valid (or none at all) fails.
func TestCheckGitHubAttestationsAnyValid(t *testing.T) {
	cosign, _ := fakeAttestCosign(t)
	policy := AttestationPolicy{Repo: "owner/tool"}
	check := func(bundles ...string) error {
		var bs [][]byte
		for _, b := range bundles {
			bs = append(bs, []byte(b))
		}
		return CheckGitHubAttestations(context.Background(), cosign, "a.tar.gz", bs, policy)
	}

	if err := check("bad", "good"); err != nil {
		t.Errorf("one valid attestation: error = %v, want nil", err)
	}
	if err := check("bad", "bad"); !errors.Is(err, ErrVerification) || !strings.Contains(err.Error(), "bad attestation") {
		t.Errorf("no valid attestation: error = %v, want ErrVerification with cosign's message", err)
	}
	if err := check(); !errors.Is(err, ErrVerification) || !strings.Contains(err.Error(), "no GitHub attestation") {
		t.Errorf("no attestation: error = %v, want ErrVerification", err)
	}
}

// TestCheckGitHubAttestationsRealGH verifies the real attestation of a gh
// release with the real cosign. It needs network access to GitHub (API and
// release download; set GITHUB_TOKEN to avoid the rate limit), so it only
// runs when PAQ_E2E_ATTESTATION points at a cosign binary.
func TestCheckGitHubAttestationsRealGH(t *testing.T) {
	cosign := os.Getenv("PAQ_E2E_ATTESTATION")
	if cosign == "" {
		t.Skip("PAQ_E2E_ATTESTATION not set")
	}
	ctx := context.Background()
	artifact, err := download.ToTemp(ctx, nil, "https://github.com/cli/cli/releases/download/v2.60.0/gh_2.60.0_linux_amd64.tar.gz", nil)
	if err != nil {
		t.Fatalf("download gh: %v", err)
	}
	defer os.Remove(artifact)
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	bundles, err := backend.FetchAttestationBundles(ctx, nil, "cli/cli", hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("fetch attestations: %v", err)
	}

	if err := CheckGitHubAttestations(ctx, cosign, artifact, bundles, AttestationPolicy{Repo: "cli/cli"}); err != nil {
		t.Errorf("expected a valid attestation, got error: %v", err)
	}
	if err := CheckGitHubAttestations(ctx, cosign, artifact, bundles, AttestationPolicy{Repo: "someone/else"}); !errors.Is(err, ErrVerification) {
		t.Errorf("wrong repo: error = %v, want ErrVerification", err)
	}
}
