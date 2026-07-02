package template

// MetaTemplates is a map of named templates (e.g. "rust_target" → "{{arch}}-{{vendor}}-{{os}}-{{env}}").
type MetaTemplates map[string]string

// Expand expands the meta-templates, adding them to v.Extra.
// osOverrides is an OS → MetaTemplates map for per-OS overrides (e.g. darwin
// has rust_target without {{env}}). Meta-templates are expanded in the order
// of the global map, then the per-OS overrides are applied.
func Expand(mt MetaTemplates, osOverrides map[string]MetaTemplates, v Vars) (Vars, error) {
	if v.Extra == nil {
		v.Extra = make(map[string]string)
	}

	// Apply the global meta-templates.
	for k, tmpl := range mt {
		val, err := Resolve(tmpl, v)
		if err != nil {
			return v, err
		}
		v.Extra[k] = val
	}

	// Apply the current OS's override (overrides the globals).
	if osOverrides != nil {
		if osMT, ok := osOverrides[v.OS]; ok {
			for k, tmpl := range osMT {
				val, err := Resolve(tmpl, v)
				if err != nil {
					return v, err
				}
				v.Extra[k] = val
			}
		}
	}

	return v, nil
}
