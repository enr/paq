package template

import "maps"

// MetaTemplates is a map of named templates (e.g. "rust_target" → "{{arch}}-{{vendor}}-{{os}}-{{env}}").
type MetaTemplates map[string]string

// Expand expands the meta-templates, adding them to v.Extra.
// osOverrides is an OS → MetaTemplates map for per-OS overrides (e.g. darwin
// has rust_target without {{env}}), keyed by the canonical OS name (as in
// [templates.darwin] / [x.templates_os.darwin]) — canonicalOS selects the
// override, independently of v.OS (which may have been remapped by the spec
// and only feeds {{os}} substitution).
//
// Meta-templates cannot reference other meta-templates; such a reference
// fails with unknown placeholder. Every template (global or per-OS override)
// is resolved against a snapshot of the incoming vars, so the result never
// depends on Go's random map iteration order.
func Expand(mt MetaTemplates, osOverrides map[string]MetaTemplates, canonicalOS string, v Vars) (Vars, error) {
	if v.Extra == nil {
		v.Extra = make(map[string]string)
	}

	// base is a snapshot of the incoming vars: templates resolve against it,
	// never against values produced during this same Expand call. Extra must
	// be its own copy since v.Extra is a shared map and the loops below write
	// into v.Extra.
	base := v
	base.Extra = maps.Clone(v.Extra)

	// Apply the global meta-templates.
	for k, tmpl := range mt {
		val, err := Resolve(tmpl, base)
		if err != nil {
			return v, err
		}
		v.Extra[k] = val
	}

	// Apply the current OS's override (overrides the globals; also resolved
	// against base, so overrides cannot reference globals either).
	if osOverrides != nil {
		if osMT, ok := osOverrides[canonicalOS]; ok {
			for k, tmpl := range osMT {
				val, err := Resolve(tmpl, base)
				if err != nil {
					return v, err
				}
				v.Extra[k] = val
			}
		}
	}

	return v, nil
}
