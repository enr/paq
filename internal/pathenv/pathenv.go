// Package pathenv modifies the user's persistent PATH environment variable.
// Only Windows is supported: there the user PATH lives in the HKCU\Environment
// registry key and can be changed without elevated privileges. On Unix the
// PATH is set by shell profiles, which paq does not edit.
package pathenv

import "strings"

// listContains reports whether dir appears as an entry of the
// semicolon-separated Windows PATH list. Comparison is case-insensitive and
// ignores surrounding quotes and trailing path separators, matching how
// Windows treats PATH entries.
func listContains(pathList, dir string) bool {
	want := normalize(dir)
	for _, entry := range strings.Split(pathList, ";") {
		if normalize(entry) == want {
			return true
		}
	}
	return false
}

func normalize(entry string) string {
	entry = strings.TrimSpace(entry)
	entry = strings.Trim(entry, `"`)
	entry = strings.TrimRight(entry, `\/`)
	return strings.ToLower(entry)
}
