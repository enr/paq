package template

// MetaTemplates è una mappa di template nominati (es. "rust_target" → "{{arch}}-{{vendor}}-{{os}}-{{env}}").
type MetaTemplates map[string]string

// Expand espande i meta-template aggiungendoli a v.Extra.
// osOverrides è una mappa OS → MetaTemplates per override per-OS (es. darwin ha rust_target senza {{env}}).
// I meta-template vengono espansi nell'ordine della mappa globale, poi applicati gli override per OS.
func Expand(mt MetaTemplates, osOverrides map[string]MetaTemplates, v Vars) (Vars, error) {
	if v.Extra == nil {
		v.Extra = make(map[string]string)
	}

	// Applica i meta-template globali
	for k, tmpl := range mt {
		val, err := Resolve(tmpl, v)
		if err != nil {
			return v, err
		}
		v.Extra[k] = val
	}

	// Applica override per OS corrente (sovrascrive i globali)
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
