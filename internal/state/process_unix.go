//go:build !windows

package state

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the given PID exists.
// Signal 0 performs the existence check without delivering anything; EPERM
// means the process exists but belongs to another user.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
