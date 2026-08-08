package state

import "syscall"

// stillActive is Windows' STILL_ACTIVE exit code, returned by
// GetExitCodeProcess for a process that has not terminated yet.
const stillActive = 259

// processAlive reports whether a process with the given PID exists.
// A handle can still be opened for an already terminated process, so the exit
// code decides.
func processAlive(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)

	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true // cannot tell: assume the owner is alive
	}
	return code == stillActive
}
