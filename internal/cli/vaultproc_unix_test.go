//go:build linux || darwin

package cli

import "syscall"

// processAlive reports whether the process pid still runs.
func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// killProcess ends the process pid at once.
func killProcess(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }
