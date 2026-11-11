//go:build windows

package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach starts cmd without the console and the process group of the test program.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}

// programOf returns the program file pid executes.
func programOf(pid int) (string, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(process, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}
