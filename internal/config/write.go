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

	if err := replaceManifest(path, content); err != nil {
		return "", err
	}

	return path, nil
}

// SetManifestAppVersion sets the version of the existing app `key` in the
// user manifest, rewriting only the `version` line of its [apps.<key>]
// table (or adding one right below the header): the rest of the file,
// comments included, is left untouched. Fails if the manifest has no
// [apps.<key>] table header, e.g. an app declared as an inline table.
// Returns the manifest's path.
func SetManifestAppVersion(key, version string) (string, error) {
	path, err := userConfigPath()
	if err != nil {
		return "", err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read manifest %s: %w", path, err)
	}

	newLine := fmt.Sprintf("version = %q", version)
	lines := strings.Split(string(existing), "\n")
	header, inTable, done := -1, false, false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			if inTable {
				break // left the table without finding a version line
			}
			if strings.TrimSpace(t[1:len(t)-1]) == "apps."+key {
				header, inTable = i, true
			}
			continue
		}
		if name, _, ok := strings.Cut(t, "="); inTable && ok && strings.TrimSpace(name) == "version" {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			if strings.HasSuffix(line, "\r") {
				newLine += "\r"
			}
			lines[i] = indent + newLine
			done = true
			break
		}
	}
	if header < 0 {
		return "", fmt.Errorf("no [apps.%s] table in %s: set its version there by hand", key, path)
	}
	if !done {
		lines = append(lines[:header+1], append([]string{newLine}, lines[header+1:]...)...)
	}
	content := strings.Join(lines, "\n")

	// Validate the result before writing, and that the edit landed where meant.
	var raw userConfigRaw
	if err := toml.Unmarshal([]byte(content), &raw); err != nil {
		return "", fmt.Errorf("resulting manifest is invalid: %w", err)
	}
	if got := raw.Apps[key].Version; got != version {
		return "", fmt.Errorf("could not set the version of %q in %s: set it there by hand", key, path)
	}

	if err := replaceManifest(path, content); err != nil {
		return "", err
	}
	return path, nil
}

// replaceManifest atomically replaces the manifest at path with content
// (temp file + rename).
func replaceManifest(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
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
