package verify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
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
