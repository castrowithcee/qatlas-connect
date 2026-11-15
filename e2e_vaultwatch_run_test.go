//go:build linux || darwin || windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const helperEnv = "QCONNECT_E2E_WATCHDOG_HELPER"

// TestWatchdogHelperRun is the aborted test program of TestWatchdogEndsRegisteredProcesses: it records a
// process and dies without any cleanup. It does nothing in a regular run.
func TestWatchdogHelperRun(t *testing.T) {
	if os.Getenv(helperEnv) == "" {
		t.Skip("runs only as the helper of TestWatchdogEndsRegisteredProcesses")
	}
	sleeper, err := startSleeper()
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	registerVaultProcess(sleeper.Process.Pid, exe)
	os.Stdout.WriteString("helper " + strconv.Itoa(sleeper.Process.Pid) + " " + strconv.Itoa(watchdog.Process.Pid) +
		" " + pidRegistry + "\n")
	os.Exit(3)
}

func waitEnded(t *testing.T, pid int, what string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); processAlive(pid); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Errorf("%s %d still runs", what, pid)
			return
		}
	}
}

func TestWatchdogEndsRegisteredProcesses(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestWatchdogHelperRun$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	out, err := cmd.Output()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 3 {
		t.Fatalf("helper run: %v, output %q, want exit 3", err, out)
	}
	var fields []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "helper ") {
			fields = strings.Fields(line)
		}
	}
	if len(fields) != 4 {
		t.Fatalf("helper output %q names no processes", out)
	}
	sleeperPID, _ := strconv.Atoi(fields[1])
	watchdogPID, _ := strconv.Atoi(fields[2])
	registry := fields[3]
	// Whatever happens, no stand-in outlives the test.
	t.Cleanup(func() {
		if processAlive(sleeperPID) && sameProgram(sleeperPID, exe) {
			killProcess(sleeperPID)
		}
	})

	waitEnded(t, sleeperPID, "the registered process")
	waitEnded(t, watchdogPID, "the watchdog")
	if _, err := os.Stat(registry); err == nil {
		t.Errorf("the registry %s was not removed", registry)
	}
}

func TestWatchdogLeavesForeignProcessAlone(t *testing.T) {
	dir := t.TempDir()
	sleeper, err := startSleeper()
	if err != nil {
		t.Fatal(err)
	}
	pid := sleeper.Process.Pid
	ended := make(chan struct{})
	go func() { _ = sleeper.Wait(); close(ended) }()
	t.Cleanup(func() { killProcess(pid); <-ended })

	// The entry claims another program, as if the PID had been reused after the recorded process ended.
	entry := dir + string(os.PathSeparator) + strconv.Itoa(pid)
	if err := os.WriteFile(entry, []byte(dir+string(os.PathSeparator)+"another-program"), 0o600); err != nil {
		t.Fatal(err)
	}
	reapRegistry(dir)
	select {
	case <-ended:
		t.Fatalf("process %d runs another program than recorded and was ended", pid)
	case <-time.After(500 * time.Millisecond):
	}
	if _, err := os.Stat(entry); err == nil {
		t.Error("the stale entry was not removed")
	}

	// With the right program recorded, the same process is ended, which shows the check can pass.
	exe, _ := os.Executable()
	if err := os.WriteFile(entry, []byte(exe), 0o600); err != nil {
		t.Fatal(err)
	}
	reapRegistry(dir)
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatalf("process %d runs the recorded program and was not ended", pid)
	}
}
