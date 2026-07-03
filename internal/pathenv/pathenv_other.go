//go:build !windows

package pathenv

import "errors"

// AddToUserPath is only implemented on Windows, where the user PATH is stored
// in the registry. On other systems the PATH comes from shell profiles, which
// paq does not edit.
func AddToUserPath(_ string) (bool, error) {
	return false, errors.New("automatic PATH setup is only supported on Windows; add the bin dir to your shell profile instead")
}
