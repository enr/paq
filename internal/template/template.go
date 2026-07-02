package template

import (
	"fmt"
	"regexp"
	"strings"
)

// Vars contains all the placeholders available for template resolution.
type Vars struct {
	OS           string
	Arch         string
	Vendor       string
	Env          string
	Ext          string
	Version      string
	VersionMajor string
	VersionMinor string
	VersionPatch string
	VersionBuild string
	// Extra contains additional placeholders (e.g. meta-templates like rust_target).
	Extra map[string]string
}

var placeholderRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Resolve substitutes all {{placeholder}} occurrences in s with the values in v.
// Returns an error if a placeholder is not recognized.
func Resolve(s string, v Vars) (string, error) {
	var resolveErr error
	result := placeholderRe.ReplaceAllStringFunc(s, func(match string) string {
		if resolveErr != nil {
			return match
		}
		name := strings.TrimSpace(match[2 : len(match)-2])
		val, err := lookup(name, v)
		if err != nil {
			resolveErr = err
			return match
		}
		return val
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return result, nil
}

func lookup(name string, v Vars) (string, error) {
	// Look up the extra placeholders first (meta-templates).
	if v.Extra != nil {
		if val, ok := v.Extra[name]; ok {
			return val, nil
		}
	}
	switch name {
	case "os":
		return v.OS, nil
	case "arch":
		return v.Arch, nil
	case "vendor":
		return v.Vendor, nil
	case "env":
		return v.Env, nil
	case "ext":
		return v.Ext, nil
	case "version":
		return v.Version, nil
	case "version_major":
		return v.VersionMajor, nil
	case "version_minor":
		return v.VersionMinor, nil
	case "version_patch":
		return v.VersionPatch, nil
	case "version_build":
		return v.VersionBuild, nil
	}
	return "", fmt.Errorf("unknown placeholder %q", name)
}
