package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// knownOSNames è la lista degli OS supportati, usata per distinguere
// le sezioni per-OS (es. [jdk.darwin]) da altri campi della spec.
var knownOSNames = map[string]bool{
	"linux":   true,
	"darwin":  true,
	"windows": true,
}

// templatesRaw è la struttura del file templates.toml
type templatesRaw struct {
	Templates map[string]any `toml:"templates"`
}

// LoadGlobalTemplates carica i meta-template globali da templates.toml.
// Ritorna (global, osOverrides).
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
			// Sezione per-OS (es. [templates.darwin])
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

// LoadEmbeddedRegistry carica tutte le spec dai file .toml nell'FS embedded.
// Ignora templates.toml (gestito separatamente).
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
			specs[k] = v
		}
	}

	return specs, nil
}

// parseSpecFile fa il parse di un file TOML di ricetta.
// Gestisce le sezioni per-OS (es. [jdk.darwin]) estraendole come OSOverrides.
func parseSpecFile(data []byte) (map[string]Spec, error) {
	// Prima passa: decode in mappa generica per gestire sezioni per-OS
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return parseSpecsFromRaw(raw)
}

// parseSpecsFromRaw converte una mappa nome→tabella-spec (già decodificata da
// TOML in forma generica) in Spec, estraendo le sezioni per-OS (es. [x.darwin])
// come OSOverrides. È condivisa dal parsing della registry embedded e da quello
// delle spec definite dall'utente nel manifest (sezione [specs.*]).
func parseSpecsFromRaw(raw map[string]any) (map[string]Spec, error) {
	result := make(map[string]Spec)

	for specName, specVal := range raw {
		specMap, ok := specVal.(map[string]any)
		if !ok {
			continue
		}

		// Estrai le sezioni per-OS prima del re-encode
		osOverrides := make(map[string]PlatformOverride)
		cleanMap := make(map[string]any)

		for k, v := range specMap {
			if knownOSNames[k] {
				// Sezione per-OS: decodifica in PlatformOverride
				overrideData, err := json.Marshal(v)
				if err != nil {
					return nil, fmt.Errorf("marshal os override %q: %w", k, err)
				}
				var ov PlatformOverride
				if err := json.Unmarshal(overrideData, &ov); err != nil {
					return nil, fmt.Errorf("unmarshal os override %q: %w", k, err)
				}
				// strip_components come *int richiede un workaround:
				// leggiamo il campo raw dal map
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

		// Re-encode la mappa pulita e fai decode in Spec
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

// userConfigPath ritorna il path del file di configurazione utente.
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

// userConfigRaw è la struttura TOML del manifest utente.
type userConfigRaw struct {
	Apps     map[string]AppEntry `toml:"apps"`
	Defaults Defaults            `toml:"defaults"`
	// Specs raccoglie le ricette definite dall'utente nella sezione [specs.*],
	// nello stesso formato dei file della registry embedded. Decodificate in
	// forma generica e poi convertite in Spec da parseSpecsFromRaw.
	Specs map[string]any `toml:"specs"`
}

// LoadUserConfig carica il manifest da ~/.config/paq/config.toml.
// Ritorna Config vuoto (senza errore) se il file non esiste.
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

	return &Config{Apps: raw.Apps, Defaults: raw.Defaults, Specs: specs}, nil
}

// Merge unisce la registry embedded con il manifest utente.
// Ritorna la Config completa pronta all'uso.
func Merge(embeddedSpecs map[string]Spec, user *Config) (*Config, error) {
	cfg := &Config{
		Specs: make(map[string]Spec, len(embeddedSpecs)),
		Apps:  make(map[string]AppEntry),
	}

	for k, v := range embeddedSpecs {
		cfg.Specs[k] = v
	}

	if user != nil {
		// Le ricette definite dall'utente ([specs.*]) sovrascrivono quelle
		// embedded con lo stesso nome (last-write-wins): permette di aggiungere
		// nuovi tool e di correggere una ricetta embedded obsoleta senza rilascio.
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
