package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// UserManifestPath ritorna il path del manifest utente (config.toml).
// È l'accessor pubblico di userConfigPath.
func UserManifestPath() (string, error) {
	return userConfigPath()
}

// WriteManifestEntry aggiunge al manifest utente il blocco TOML `block` (es. una
// sezione "[apps.<key>]"), creando file e directory se non esistono.
//
// Se overwrite è true ed esiste già una tabella per `key`, questa viene rimossa
// prima di aggiungere il nuovo blocco (i commenti del resto del file restano
// intatti). Se overwrite è false e la chiave esiste già, la validazione fallisce
// e il manifest non viene toccato.
//
// La scrittura è difensiva: il contenuto risultante viene prima validato con un
// parse TOML e solo dopo scritto su disco in modo atomico (file temporaneo +
// rename). Ritorna il path del manifest.
func WriteManifestEntry(key, block string, overwrite bool) (string, error) {
	path, err := userConfigPath()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read manifest %s: %w", path, err)
	}

	content := string(existing)
	if overwrite {
		content = removeAppTable(content, key)
	}

	content = strings.TrimRight(content, "\n")
	if content != "" {
		content += "\n\n"
	}
	content += strings.TrimRight(block, "\n") + "\n"

	// Valida il risultato prima di scrivere: una chiave [apps.<key>] duplicata
	// fa fallire l'unmarshal, evitando di corrompere il manifest.
	var raw userConfigRaw
	if err := toml.Unmarshal([]byte(content), &raw); err != nil {
		return "", fmt.Errorf("resulting manifest is invalid (entry %q may already exist): %w", key, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("replace manifest: %w", err)
	}

	return path, nil
}

// removeAppTable rimuove dal contenuto TOML le tabelle relative all'app `key`
// (sia "[apps.<key>]" sia eventuali sottotabelle "[apps.<key>.xxx]"), lasciando
// inalterato il resto del file. Opera per righe per non riformattare il manifest.
func removeAppTable(content, key string) string {
	target := "apps." + key
	prefix := target + "."

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			header := strings.TrimSpace(t[1 : len(t)-1])
			skipping = header == target || strings.HasPrefix(header, prefix)
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
