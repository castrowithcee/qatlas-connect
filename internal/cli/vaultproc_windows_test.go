//go:build windows

package cli

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processAlive reports whether the process pid still runs.
func processAlive(pid int) bool {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	event, err := windows.WaitForSingleObject(process, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}

// killProcess ends the process pid at once.
func killProcess(pid int) {
	process, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(process)
	_ = windows.TerminateProcess(process, 1)
}

// processArguments returns the command line of the process pid, which a hardened vault process still lets
// this user read.
func processArguments(t *testing.T, pid int) string {
	t.Helper()
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatalf("cannot open process %d: %v", pid, err)
	}
	defer windows.CloseHandle(process)
	buf := make([]byte, 64<<10)
	var size uint32
	if err := windows.NtQueryInformationProcess(process, windows.ProcessCommandLineInformation,
		unsafe.Pointer(&buf[0]), uint32(len(buf)), &size); err != nil {
		t.Fatalf("cannot read the command line of process %d: %v", pid, err)
	}
	return (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0])).String()
}
