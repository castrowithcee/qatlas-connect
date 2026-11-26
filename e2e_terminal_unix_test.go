//go:build linux || darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// processAlive reports whether the process pid still runs.
func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// killProcess ends the process pid at once.
func killProcess(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }

// runAtTerminal runs the binary with a pseudo-terminal as its controlling terminal and standard streams,
// types input once the passphrase prompt appeared, and returns everything it wrote there and its exit code.
func runAtTerminal(t *testing.T, c *runner, input string, args ...string) (string, int) {
	t.Helper()
	out, code, _, err := runAtTerminalWithin(t, c, terminalRunLimit, input, args...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return out, code
}

// runAtTerminalWithin is runAtTerminal with a limit for the program to end after the input was typed. Past
// it the program is killed, and the error carries what it wrote so far. pid is the program's process id.
func runAtTerminalWithin(t *testing.T, c *runner, limit time.Duration, input string, args ...string) (
	result string, code, pid int, err error) {
	t.Helper()
	master, slave := openPTY(t)
	defer master.Close()
	cmd := exec.Command(c.bin, args...)
	cmd.Env = c.environ(t)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %v: %v", args, err)
	}
	_ = slave.Close()

	var mu sync.Mutex
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			out.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				// EIO once the last process holding the terminal ended.
				return
			}
		}
	}()
	text := func() string { mu.Lock(); defer mu.Unlock(); return out.String() }

	// The prompt is written before the echo is switched off, so typing waits for both: input typed in
	// between would be echoed back and look like a leaked passphrase.
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(text(), "passphrase: ") || !echoOff(master); {
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("no passphrase prompt with echo off appeared: %q", text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := io.WriteString(master, input); err != nil {
		t.Fatalf("typing at the terminal: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-exited:
	case <-time.After(limit):
		_ = cmd.Process.Kill()
		<-exited
		// Killing ends the terminal's last holder, so the reader returns with what was written.
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		c.seen.WriteString(text())
		return text(), -1, cmd.Process.Pid, fmt.Errorf("running %v did not end within %v: %q", args, limit, text())
	}
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) {
		code = exit.ExitCode()
	} else if waitErr != nil {
		t.Fatalf("running %v: %v", args, waitErr)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	c.seen.WriteString(text())
	return text(), code, cmd.Process.Pid, nil
}
