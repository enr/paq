package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// spawnDetached launches "paq __update-check" as a detached background process
// that keeps running after the current command exits. Its stdio is discarded;
// any failure is ignored (the check is best-effort).
func spawnDetached() {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer devNull.Close()

	cmd := exec.Command(exe, "__update-check")
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	// Detach from the console so the worker survives the parent exiting.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}

	if err := cmd.Start(); err != nil {
		return
	}
	// Do not Wait: let the worker run independently.
	_ = cmd.Process.Release()
}
