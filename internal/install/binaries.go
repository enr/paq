package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/enr/paq/internal/archive"
)

// ResolvedBinary è un eseguibile da installare con i template già risolti.
// From è il basename del file nell'archivio (vuoto per i download nudi);
// To è il nome con cui installarlo.
type ResolvedBinary struct {
	From string
	To   string
}

// InstallBinaries installa uno o più eseguibili in destDir, applicando chmod a
// ciascuno. Ritorna i path assoluti dei file installati (per lo state).
//
// Se archiveType è vuoto l'artefatto scaricato È l'eseguibile (download nudo):
// è ammessa una sola entry e l'artefatto viene installato come destDir/<To>.
// Altrimenti ogni binario viene estratto dall'archivio (per basename From) in
// una temp dir sullo stesso filesystem di destDir, poi spostato in destDir/<To>.
func InstallBinaries(artifactPath, archiveType string, bins []ResolvedBinary, destDir, chmod string, opts archive.ExtractOpts) ([]string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}

	mode, err := parseFileMode(chmod)
	if err != nil {
		return nil, err
	}

	// Download nudo: l'artefatto è il binario, nessuna estrazione.
	if archiveType == "" {
		if len(bins) != 1 {
			return nil, fmt.Errorf("a non-archive download installs exactly one binary, got %d", len(bins))
		}
		dest := filepath.Join(destDir, bins[0].To)
		if err := installRawBinary(artifactPath, dest, mode); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	}

	tmpDir, err := os.MkdirTemp(destDir, "paq-install-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Estrai tutti i binari in temp e applica chmod, prima di toccare destDir.
	for _, b := range bins {
		if b.From == "" {
			return nil, fmt.Errorf("binaries: 'from' is required when 'archive' is set")
		}
		eopts := opts
		eopts.Extract = b.From
		eopts.Dest = tmpDir
		if err := archive.Extract(artifactPath, archiveType, eopts); err != nil {
			return nil, fmt.Errorf("extract %q: %w", b.From, err)
		}
		extracted := filepath.Join(tmpDir, b.From)
		if mode != 0 {
			if err := os.Chmod(extracted, mode); err != nil {
				return nil, fmt.Errorf("chmod %q: %w", b.From, err)
			}
		}
	}

	// Sposta ogni binario in destDir/<To>.
	var installed []string
	for _, b := range bins {
		extracted := filepath.Join(tmpDir, b.From)
		dest := filepath.Join(destDir, b.To)
		if err := os.Rename(extracted, dest); err != nil {
			return installed, fmt.Errorf("install %q: %w", b.To, err)
		}
		installed = append(installed, dest)
	}

	return installed, nil
}

// installRawBinary copia l'artefatto in dest con swap atomico nella stessa dir,
// applicando mode (l'artefatto può trovarsi su un filesystem diverso da dest).
func installRawBinary(src, dest string, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), "paq-install-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	in, err := os.Open(src)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("open artifact: %w", err)
	}
	_, copyErr := io.Copy(tmp, in)
	in.Close()
	tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("copy artifact: %w", copyErr)
	}

	if mode != 0 {
		if err := os.Chmod(tmpName, mode); err != nil {
			return fmt.Errorf("chmod: %w", err)
		}
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	return nil
}

// parseFileMode interpreta un chmod ottale (es. "0755"); "" → 0 (nessun chmod).
func parseFileMode(chmod string) (os.FileMode, error) {
	if chmod == "" {
		return 0, nil
	}
	m, err := strconv.ParseUint(chmod, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("parse chmod %q: %w", chmod, err)
	}
	return os.FileMode(m), nil
}
