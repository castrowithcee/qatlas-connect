//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"
)

// programOf returns the program file pid executes. The kernel appends " (deleted)" once an update replaced
// the file, which the recorded path does not carry.
func programOf(pid int) (string, error) {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return strings.TrimSuffix(exe, " (deleted)"), err
}
