//go:build !windows

package main

import (
	"os/exec"
)

// detachConsole is a no-op off Windows (no console windows exist there;
// children never share a closable console with the parent).
func detachConsole(cmd *exec.Cmd) {
}
