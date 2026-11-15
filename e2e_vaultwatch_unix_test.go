//go:build linux || darwin

package main

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a session of its own, so it outlives the test program.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
