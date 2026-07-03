package pathenv

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// AddToUserPath appends dir to the current user's persistent PATH, stored in
// the HKCU\Environment registry key, which needs no elevated privileges and
// affects only this user. It returns false if dir is already present.
// On success it broadcasts WM_SETTINGCHANGE so programs started afterwards
// see the new PATH; already-running shells must be restarted.
func AddToUserPath(dir string) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, fmt.Errorf("open HKCU\\Environment: %w", err)
	}
	defer key.Close()

	current, valType, err := key.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return false, fmt.Errorf("read user Path: %w", err)
	}
	if listContains(current, dir) {
		return false, nil
	}

	updated := dir
	if trimmed := strings.TrimRight(current, ";"); trimmed != "" {
		updated = trimmed + ";" + dir
	}
	// Keep REG_EXPAND_SZ unless the existing value is plain REG_SZ, so
	// entries containing %VAR% references keep expanding.
	if valType == registry.SZ {
		err = key.SetStringValue("Path", updated)
	} else {
		err = key.SetExpandStringValue("Path", updated)
	}
	if err != nil {
		return false, fmt.Errorf("write user Path: %w", err)
	}

	broadcastEnvChange()
	return true, nil
}

// broadcastEnvChange notifies running programs (notably Explorer) that the
// environment changed, so terminals opened after this call inherit the new
// PATH without a logoff. Failures are ignored: the registry write, which is
// what persists the change, already succeeded.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, 0)
}
