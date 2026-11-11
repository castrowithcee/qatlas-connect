//go:build linux || darwin || windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// A vault process outlives the program that started it, and the e2e tests start them detached. When the test
// program is killed (go test timeout, kill from outside) no cleanup runs, so TestMain starts a watchdog: the
// test program run again in a mode of its own, detached. The tests record every vault process they learn of
// in a registry directory; once the test program is gone, the watchdog ends the processes recorded there.
//
// A registry entry is a file named after the PID whose content is the program the process must still run.
// The watchdog ends a process only if the entry exists and the PID still executes exactly that program, so a
// PID the system reused for another program stays untouched.

const (
	roleEnv       = "QCONNECT_E2E_ROLE"
	registryEnv   = "QCONNECT_E2E_REGISTRY"
	parentEnv     = "QCONNECT_E2E_PARENT"
	roleWatchdog  = "watchdog"
	roleSleeper   = "sleeper"
	watchInterval = 250 * time.Millisecond
	// A sleeper stands in for a vault process in the watchdog tests; it ends by itself if nothing ends it.
	sleeperLifetime = 2 * time.Minute
)

var (
	// pidRegistry is the registry directory of this run.
	pidRegistry string
	// watchdog is the watchdog process of this run, nil in the watchdog itself.
	watchdog *exec.Cmd
)

var unlockedPIDPattern = regexp.MustCompile(`unlocked in a vault process \(pid (\d+)\)`)

// vaultPIDFromOutput returns the PID `vault unlock` reports in out.
func vaultPIDFromOutput(out string) (int, bool) {
	match := unlockedPIDPattern.FindStringSubmatch(out)
	if match == nil {
		return 0, false
	}
	pid, err := strconv.Atoi(match[1])
	return pid, err == nil
}

// unlockedVaultPID reads the PID from the output of `vault unlock` and records it in the registry as a
// process of program. It returns false if out names no PID.
func unlockedVaultPID(out, program string) (int, bool) {
	pid, ok := vaultPIDFromOutput(out)
	if ok {
		registerVaultProcess(pid, program)
	}
	return pid, ok
}

// registerVaultProcess records pid as a process of program, which must be a path below the run's temp area.
func registerVaultProcess(pid int, program string) {
	if pidRegistry == "" || pid <= 0 {
		return
	}
	if resolved, err := filepath.EvalSymlinks(program); err == nil {
		program = resolved
	}
	entry := filepath.Join(pidRegistry, strconv.Itoa(pid))
	// Written aside and renamed, so the watchdog never reads a half-written entry.
	tmp := entry + ".tmp"
	if err := os.WriteFile(tmp, []byte(program), 0o600); err == nil {
		_ = os.Rename(tmp, entry)
	}
}

// unregisterVaultProcess removes the entry of a process that ended regularly.
func unregisterVaultProcess(pid int) {
	if pidRegistry != "" {
		_ = os.Remove(filepath.Join(pidRegistry, strconv.Itoa(pid)))
	}
}

// sameProgram reports whether pid currently executes the program at want.
func sameProgram(pid int, want string) bool {
	got, err := programOf(pid)
	if err != nil || got == "" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	got, want = filepath.Clean(got), filepath.Clean(want)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(got, want)
	}
	return got == want
}

// reapRegistry ends every process recorded in dir that still runs the recorded program, and empties dir.
func reapRegistry(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		program, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err == nil && processAlive(pid) && sameProgram(pid, string(program)) {
			killProcess(pid)
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// runWatchdog runs until the test program, which holds the write end of stdin, is gone, then reaps the
// registry and removes it. The closed pipe is the reliable signal; polling the PID covers a test program that
// left the pipe open in a child.
func runWatchdog(dir string, parent int, stdin io.Reader) {
	closed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		close(closed)
	}()
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-closed:
		case <-ticker.C:
			if processAlive(parent) {
				continue
			}
		}
		reapRegistry(dir)
		_ = os.RemoveAll(dir)
		return
	}
}

// startWatchdog creates the registry and starts the detached watchdog.
func startWatchdog() error {
	dir, err := os.MkdirTemp("", "qe2e-pids")
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), roleEnv+"="+roleWatchdog, registryEnv+"="+dir, parentEnv+"="+strconv.Itoa(os.Getpid()))
	// No output pipes: a watchdog holding them would keep go test waiting for it.
	if _, err := cmd.StdinPipe(); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	pidRegistry, watchdog = dir, cmd
	return nil
}

// stopWatchdog ends the watchdog after a regular run, ends what a failed cleanup left registered and removes
// the registry.
func stopWatchdog() {
	if watchdog == nil {
		return
	}
	killProcess(watchdog.Process.Pid)
	_ = watchdog.Wait()
	reapRegistry(pidRegistry)
	_ = os.RemoveAll(pidRegistry)
}

// startSleeper starts a detached stand-in for a vault process: this program in sleeper mode. It returns its
// PID; the caller must end it.
func startSleeper() (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), roleEnv+"="+roleSleeper)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting the sleeper: %w", err)
	}
	return cmd, nil
}
