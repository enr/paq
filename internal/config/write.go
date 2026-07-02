package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// UserManifestPath returns the path of the user manifest (config.toml).
// It is the public accessor for userConfigPath.
func UserManifestPath() (string, error) {
	return userConfigPath()
}

// WriteManifestEntry appends the TOML `block` (e.g. an "[apps.<key>]" section)
// to the user manifest, creating the file and directory if they don't exist.
//
// If overwrite is true and a table for `key` already exists, it is removed
// before adding the new block (comments in the rest of the file are left
// intact). If overwrite is false and the key already exists, validation
// fails and the manifest is left untouched.
//
// Writing is defensive: the resulting content is first validated with a TOML
// parse and only then written to disk atomically (temp file + rename).
// Returns the manifest's path.
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

	// Validate the result before writing: a duplicate [apps.<key>] key
	// makes the unmarshal fail, avoiding corruption of the manifest.
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

// removeAppTable removes from the TOML content the tables for app `key`
// (both "[apps.<key>]" and any "[apps.<key>.xxx]" subtables), leaving the
// rest of the file unchanged. Operates line-by-line to avoid reformatting the manifest.
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
