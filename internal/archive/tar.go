package archive

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractTar estrae un archivio tar da reader con le opzioni specificate.
// Questa funzione è condivisa da tar.gz e tar.xz.
func extractTar(r io.Reader, opts ExtractOpts) error {
	tr := tar.NewReader(r)

	found := false // usato per modalità Extract (file singolo)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Normalizza il path e applica StripComponents
		name := filepath.ToSlash(hdr.Name)
		name = strings.TrimPrefix(name, "./")
		stripped, ok := stripComponents(name, opts.StripComponents)
		if !ok || stripped == "" {
			continue
		}

		switch {
		case opts.Extract != "":
			// Modalità file singolo: cerca il file per basename
			if filepath.Base(stripped) == opts.Extract {
				dest := filepath.Join(opts.Dest, opts.Extract)
				if err := writeFile(tr, dest, hdr.FileInfo().Mode()); err != nil {
					return err
				}
				found = true
			}

		case opts.Subdir != "":
			// Modalità sottoalbero: estrai solo i file sotto subdir (con glob sul primo segmento)
			rel, match := matchSubdir(stripped, opts.Subdir)
			if !match || rel == "" {
				continue
			}
			dest := filepath.Join(opts.Dest, filepath.FromSlash(rel))
			if hdr.Typeflag == tar.TypeDir {
				if err := os.MkdirAll(dest, 0755); err != nil {
					return err
				}
			} else {
				if err := writeFile(tr, dest, hdr.FileInfo().Mode()); err != nil {
					return err
				}
			}

		default:
			// Modalità standard: estrai tutto
			dest := filepath.Join(opts.Dest, filepath.FromSlash(stripped))
			if hdr.Typeflag == tar.TypeDir {
				if err := os.MkdirAll(dest, 0755); err != nil {
					return err
				}
			} else {
				if err := writeFile(tr, dest, hdr.FileInfo().Mode()); err != nil {
					return err
				}
			}
		}
	}

	if opts.Extract != "" && !found {
		return fmt.Errorf("file %q not found in archive", opts.Extract)
	}
	return nil
}

// stripComponents rimuove i primi n componenti del path.
// Ritorna ("", false) se il path ha meno di n componenti.
func stripComponents(path string, n int) (string, bool) {
	if n <= 0 {
		return path, true
	}
	parts := strings.SplitN(path, "/", n+1)
	if len(parts) <= n {
		return "", false
	}
	return parts[n], true
}

// matchSubdir verifica se path inizia con il prefisso subdir (che può avere "*" come glob nel primo segmento).
// Se c'è match, ritorna il path relativo rispetto a subdir.
func matchSubdir(path, subdir string) (rel string, ok bool) {
	subdirParts := strings.Split(strings.TrimSuffix(subdir, "/"), "/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) < len(subdirParts) {
		return "", false
	}

	for i, sp := range subdirParts {
		pp := pathParts[i]
		if sp == "*" {
			continue // glob: qualsiasi segmento va bene
		}
		if sp != pp {
			return "", false
		}
	}

	rel = strings.Join(pathParts[len(subdirParts):], "/")
	return rel, true
}

// writeFile scrive il contenuto del reader nel file dest, creando le directory necessarie.
func writeFile(r io.Reader, dest string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode&0777|0200)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	// Applica i permessi corretti dopo la scrittura
	return os.Chmod(dest, mode&0777|0200)
}
