//go:build darwin

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// programOf returns the program file pid executes; on macOS ps prints the full path as comm.
func programOf(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	return strings.TrimSpace(string(out)), err
}
