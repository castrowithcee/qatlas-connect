//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A program that prompts, switches the echo off and then never ends fails the run at the limit, with the
// output so far, and is gone afterwards.
func TestRunAtTerminalLimit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hang.sh")
	body := "#!/bin/sh\nprintf 'vault passphrase: '\nstty -echo\necho $$ > " + filepath.Join(dir, "pid") + "\nexec sleep 600\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	c := &runner{bin: script, env: []string{"QATLAS_E2E_CA_FILE=unused", "PATH=" + os.Getenv("PATH")}, seen: &strings.Builder{}}

	start := time.Now()
	out, _, pid, err := runAtTerminalWithin(t, c, 500*time.Millisecond, "secret\n")
	if err == nil {
		t.Fatalf("a program that never ends returned no error, output %q", out)
	}
	if time.Since(start) > 30*time.Second {
		t.Errorf("the wait took %v, want about the limit", time.Since(start))
	}
	if !strings.Contains(err.Error(), "vault passphrase: ") {
		t.Errorf("error %q lacks the terminal output", err)
	}
	if data, readErr := os.ReadFile(filepath.Join(dir, "pid")); readErr != nil ||
		strings.TrimSpace(string(data)) != strconv.Itoa(pid) {
		t.Errorf("script pid file = %q, %v; want %d", data, readErr, pid)
	}
	if processAlive(pid) {
		t.Errorf("process %d still runs after the limit", pid)
	}
}
