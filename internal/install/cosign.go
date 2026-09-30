package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/enr/paq/internal/config"
	"github.com/enr/paq/internal/download"
	"github.com/enr/paq/internal/registry"
	"github.com/enr/paq/internal/state"
	"github.com/enr/paq/internal/verify"
	"github.com/enr/paq/internal/version"
)

// Origins of the cosign binary returned by FindCosign.
const (
	CosignFromDefaults = "defaults.cosign"
	CosignFromPath     = "PATH"
	CosignFromState    = "installed by paq"
	CosignFromPrivate  = "paq private copy"
)

// FoundCosign is a usable cosign binary.
type FoundCosign struct {
	Path    string
	Version string // empty for the private copy (pinned, not queried)
	Origin  string
}

// PrivateCosignDir returns the directory holding paq's private cosign copies:
// <cache>/paq/tools/cosign, next to the registry snapshot.
func PrivateCosignDir() (string, error) {
	regDir, err := registry.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(regDir), "tools", "cosign"), nil
}

// privateCosignPath returns the path of the pinned private copy.
func privateCosignPath() (string, error) {
	dir, err := PrivateCosignDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cosignPinVersion, "cosign"+exeExt()), nil
}

func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// FindCosign looks for a usable cosign without any network access, in order:
// the defaults.cosign path, cosign on PATH, a cosign recorded in the state
// (installed by paq into a bin dir that may not be on PATH), and paq's private
// copy. A cosign supplied by the user wins over the private copy, but one
// older than cosignMinVersion is skipped (reported through warn).
func FindCosign(cfg *config.Config, warn func(string)) (FoundCosign, bool) {
	var candidates []FoundCosign
	if cfg.Defaults.Cosign != "" {
		candidates = append(candidates, FoundCosign{Path: cfg.Defaults.Cosign, Origin: CosignFromDefaults})
	}
	if p, err := exec.LookPath("cosign"); err == nil {
		candidates = append(candidates, FoundCosign{Path: p, Origin: CosignFromPath})
	}
	if st, err := state.Load(); err == nil {
		for _, rec := range st.ByName("cosign") {
			for _, f := range append([]string{rec.Dest}, rec.Files...) {
				if filepath.Base(f) == "cosign"+exeExt() {
					candidates = append(candidates, FoundCosign{Path: f, Origin: CosignFromState})
				}
			}
		}
	}

	for _, c := range candidates {
		v, err := cosignVersion(c.Path)
		if err != nil {
			warn(fmt.Sprintf("cosign %s (%s) is not usable: %v", c.Path, c.Origin, err))
			continue
		}
		if version.Compare(v, cosignMinVersion) < 0 {
			warn(fmt.Sprintf("cosign %s (%s) is version %s, older than the required %s: skipped", c.Path, c.Origin, v, cosignMinVersion))
			continue
		}
		c.Version = v
		return c, true
	}

	if p, err := privateCosignPath(); err == nil {
		if _, err := os.Stat(p); err == nil {
			return FoundCosign{Path: p, Origin: CosignFromPrivate}, true
		}
	}
	return FoundCosign{}, false
}

// cosignVersion runs "cosign version --json" and returns the clean version.
func cosignVersion(path string) (string, error) {
	out, err := exec.Command(path, "version", "--json").Output()
	if err != nil {
		return "", err
	}
	var v struct {
		GitVersion string `json:"gitVersion"`
	}
	if err := json.Unmarshal(out, &v); err != nil || v.GitVersion == "" {
		return "", fmt.Errorf("cannot read its version")
	}
	return version.Clean(v.GitVersion), nil
}

// ErrCosignDeclined is returned when the user chose not to install cosign.
var ErrCosignDeclined = errors.New("cosign is required for keyless signature verification: installation declined")

// EnsureCosign returns a usable cosign, installing one if needed. When none
// is found, missing (if set) lets the user choose: it returns true after
// installing cosign as a regular tool, false to fall back to paq's private
// copy, or an error to abort. Without missing, the private copy is used.
func EnsureCosign(ctx context.Context, cfg *config.Config, client *http.Client, missing func(context.Context) (bool, error), step, warn func(string)) (FoundCosign, error) {
	if c, ok := FindCosign(cfg, warn); ok {
		return c, nil
	}
	if missing != nil {
		userInstalled, err := missing(ctx)
		if err != nil {
			return FoundCosign{}, err
		}
		if userInstalled {
			if c, ok := FindCosign(cfg, warn); ok {
				return c, nil
			}
			return FoundCosign{}, fmt.Errorf("cosign was installed but cannot be found or used")
		}
	}
	step(fmt.Sprintf("Installing cosign %s (paq private copy, needed for keyless signature verification)...", cosignPinVersion))
	p, err := installPrivateCosign(ctx, client)
	if err != nil {
		return FoundCosign{}, fmt.Errorf("install cosign: %w", err)
	}
	return FoundCosign{Path: p, Origin: CosignFromPrivate}, nil
}

// installPrivateCosign downloads the pinned cosign release binary, checks it
// against the pinned sha256 and moves it into the private directory.
func installPrivateCosign(ctx context.Context, client *http.Client) (string, error) {
	key := runtime.GOOS + "/" + runtime.GOARCH
	sum, ok := cosignPinSHA256[key]
	if !ok {
		return "", fmt.Errorf("no cosign build pinned for %s: install cosign yourself (on PATH) or set defaults.cosign", key)
	}
	dest, err := privateCosignPath()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/%s/cosign-%s-%s%s", cosignReleaseBase, cosignPinVersion, runtime.GOOS, runtime.GOARCH, exeExt())
	tmp, err := download.ToTemp(ctx, client, url, nil)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	if err := verify.CheckFile(tmp, sum); err != nil {
		return "", err
	}

	// Copy into the destination directory, then rename: the rename is atomic
	// (parallel installs never see a partial binary) and, unlike renaming the
	// download itself, never crosses filesystems.
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", err
	}
	part, err := os.CreateTemp(filepath.Dir(dest), ".cosign-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(part.Name())
	src, err := os.Open(tmp)
	if err != nil {
		part.Close()
		return "", err
	}
	_, err = io.Copy(part, src)
	src.Close()
	if cerr := part.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if err := os.Chmod(part.Name(), 0755); err != nil {
		return "", err
	}
	if err := os.Rename(part.Name(), dest); err != nil {
		return "", err
	}
	return dest, nil
}

// RemovePrivateCosign deletes paq's private cosign copies. Called once the
// user installs cosign as a regular tool, which takes precedence anyway.
func RemovePrivateCosign() error {
	dir, err := PrivateCosignDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
