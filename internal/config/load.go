package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// knownOSNames is the list of supported OSes, used to distinguish per-OS
// sections (e.g. [jdk.darwin]) from other spec fields.
var knownOSNames = map[string]bool{
	"linux":   true,
	"darwin":  true,
	"windows": true,
}

// templatesRaw is the structure of the templates.toml file.
type templatesRaw struct {
	Templates map[string]any `toml:"templates"`
}

// LoadGlobalTemplates loads the global meta-templates from templates.toml.
// Returns (global, osOverrides).
func LoadGlobalTemplates(registryFS fs.FS) (map[string]string, map[string]map[string]string, error) {
	data, err := fs.ReadFile(registryFS, "registry/templates.toml")
	if err != nil {
		return nil, nil, fmt.Errorf("read templates.toml: %w", err)
	}

	var raw templatesRaw
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse templates.toml: %w", err)
	}

	global := make(map[string]string)
	osOverrides := make(map[string]map[string]string)

	for k, v := range raw.Templates {
		switch tv := v.(type) {
		case string:
			global[k] = tv
		case map[string]any:
			// Per-OS section (e.g. [templates.darwin]).
			osMap := make(map[string]string)
			for mk, mv := range tv {
				if s, ok := mv.(string); ok {
					osMap[mk] = s
				}
			}
			osOverrides[k] = osMap
		}
	}

	return global, osOverrides, nil
}

// LoadEmbeddedRegistry loads all specs from the .toml files in the embedded FS.
// Ignores templates.toml (handled separately).
func LoadEmbeddedRegistry(registryFS fs.FS) (map[string]Spec, error) {
	specs := make(map[string]Spec)

	entries, err := fs.ReadDir(registryFS, "registry")
	if err != nil {
		return nil, fmt.Errorf("read embedded registry: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		if e.Name() == "templates.toml" {
			continue
		}

		data, err := fs.ReadFile(registryFS, "registry/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}

		parsed, err := parseSpecFile(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		for k, v := range parsed {
			v.Origin = OriginEmbedded
			specs[k] = v
		}
	}

	return specs, nil
}

// OverlayRegistry applies an external registry snapshot on top of the
// embedded specs and global templates, in place. Specs override by name
// (marked OriginRegistry); templates merge per key. On error nothing is
// modified, so a broken snapshot degrades to embedded-only.
func OverlayRegistry(specs map[string]Spec, global map[string]string, globalOS map[string]map[string]string, snapshotFS fs.FS) error {
	extSpecs, err := LoadEmbeddedRegistry(snapshotFS)
	if err != nil {
		return err
	}
	extGlobal, extGlobalOS, err := LoadGlobalTemplates(snapshotFS)
	if err != nil {
		// A snapshot without templates.toml keeps the embedded templates;
		// an unparsable one invalidates the whole snapshot.
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		extGlobal, extGlobalOS = nil, nil
	}

	for k, v := range extSpecs {
		v.Origin = OriginRegistry
		specs[k] = v
	}
	for k, v := range extGlobal {
		global[k] = v
	}
	for osName, m := range extGlobalOS {
		if globalOS[osName] == nil {
			globalOS[osName] = make(map[string]string, len(m))
		}
		for k, v := range m {
			globalOS[osName][k] = v
		}
	}
	return nil
}

// parseSpecFile parses a recipe TOML file.
// Handles per-OS sections (e.g. [jdk.darwin]) by extracting them as OSOverrides.
func parseSpecFile(data []byte) (map[string]Spec, error) {
	// First pass: decode into a generic map to handle per-OS sections.
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return parseSpecsFromRaw(raw)
}

// parseSpecsFromRaw converts a name→spec-table map (already decoded from TOML
// in generic form) into Spec, extracting per-OS sections (e.g. [x.darwin]) as
// OSOverrides. Shared by parsing of the embedded registry and of user-defined
// specs in the manifest ([specs.*] section).
func parseSpecsFromRaw(raw map[string]any) (map[string]Spec, error) {
	result := make(map[string]Spec)

	for specName, specVal := range raw {
		specMap, ok := specVal.(map[string]any)
		if !ok {
			continue
		}

		// Extract the per-OS sections before re-encoding.
		osOverrides := make(map[string]PlatformOverride)
		cleanMap := make(map[string]any)

		for k, v := range specMap {
			if knownOSNames[k] {
				// Per-OS section: decode into PlatformOverride.
				overrideData, err := json.Marshal(v)
				if err != nil {
					return nil, fmt.Errorf("marshal os override %q: %w", k, err)
				}
				var ov PlatformOverride
				if err := json.Unmarshal(overrideData, &ov); err != nil {
					return nil, fmt.Errorf("unmarshal os override %q: %w", k, err)
				}
				// strip_components as *int needs a workaround:
				// read the raw field from the map.
				if ovMap, ok := v.(map[string]any); ok {
					if sc, ok := ovMap["strip_components"]; ok {
						switch scv := sc.(type) {
						case int64:
							n := int(scv)
							ov.StripComponents = &n
						case float64:
							n := int(scv)
							ov.StripComponents = &n
						}
					}
				}
				osOverrides[k] = ov
			} else {
				cleanMap[k] = v
			}
		}

		// Re-encode the clean map and decode it into Spec.
		cleanData, err := toml.Marshal(map[string]any{specName: cleanMap})
		if err != nil {
			return nil, fmt.Errorf("re-encode spec %q: %w", specName, err)
		}

		var specWrapper map[string]Spec
		if err := toml.Unmarshal(cleanData, &specWrapper); err != nil {
			return nil, fmt.Errorf("decode spec %q: %w", specName, err)
		}

		spec := specWrapper[specName]
		spec.OSOverrides = osOverrides
		result[specName] = spec
	}

	return result, nil
}

// userConfigPath returns the path of the user configuration file.
func userConfigPath() (string, error) {
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", fmt.Errorf("APPDATA not set")
		}
		return filepath.Join(appdata, "paq", "config.toml"), nil
	}

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "paq", "config.toml"), nil
}

// userConfigRaw is the TOML structure of the user manifest.
type userConfigRaw struct {
	Apps     map[string]AppEntry `toml:"apps"`
	Defaults Defaults            `toml:"defaults"`
	// Specs collects the user-defined recipes in the [specs.*] section, in the
	// same format as the embedded registry files. Decoded in generic form and
	// then converted into Spec by parseSpecsFromRaw.
	Specs map[string]any `toml:"specs"`
}

// LoadUserConfig loads the manifest from ~/.config/paq/config.toml.
// Returns an empty Config (without error) if the file doesn't exist.
func LoadUserConfig() (*Config, error) {
	path, err := userConfigPath()
	if err != nil {
		return &Config{Apps: make(map[string]AppEntry)}, nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{Apps: make(map[string]AppEntry)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read user config %s: %w", path, err)
	}

	var raw userConfigRaw
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse user config: %w", err)
	}

	if raw.Apps == nil {
		raw.Apps = make(map[string]AppEntry)
	}

	specs, err := parseSpecsFromRaw(raw.Specs)
	if err != nil {
		return nil, fmt.Errorf("parse user specs: %w", err)
	}
	for k, v := range specs {
		v.Origin = OriginUser
		specs[k] = v
	}

	return &Config{Apps: raw.Apps, Defaults: raw.Defaults, Specs: specs}, nil
}

// Merge combines the embedded registry with the user manifest.
// Returns the complete, ready-to-use Config.
func Merge(embeddedSpecs map[string]Spec, user *Config) (*Config, error) {
	cfg := &Config{
		Specs: make(map[string]Spec, len(embeddedSpecs)),
		Apps:  make(map[string]AppEntry),
	}

	for k, v := range embeddedSpecs {
		cfg.Specs[k] = v
	}

	if user != nil {
		// User-defined recipes ([specs.*]) override embedded ones with the
		// same name (last-write-wins): this allows adding new tools and
		// patching a stale embedded recipe without a release.
		for k, v := range user.Specs {
			cfg.Specs[k] = v
		}
		for k, v := range user.Apps {
			cfg.Apps[k] = v
		}
		cfg.Defaults = user.Defaults
		if user.GlobalTemplates != nil {
			cfg.GlobalTemplates = user.GlobalTemplates
		}
		if user.GlobalTemplatesOS != nil {
			cfg.GlobalTemplatesOS = user.GlobalTemplatesOS
		}
	}

	return cfg, nil
}
