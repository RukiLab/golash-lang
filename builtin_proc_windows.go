//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachConsole starts cmd without a console window (DETACHED_PROCESS).
// A console-subsystem child otherwise shares (double-click launch) or
// inherits (terminal launch) the parent console: closing that console
// window kills every attached process, so a GUI editor that runs a
// script would die together with its child. Capture-style builtins
// (piperun/pipeexec) use pipes and never need the console; the
// interactive exec() keeps it (see execgui() for the detached twin).
func detachConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// DETACHED_PROCESS: no console is created or inherited.
		CreationFlags: 0x08000000,
	}
}
