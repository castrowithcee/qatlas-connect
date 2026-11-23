//go:build linux

package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

// TestVaultLockElsewhereLocksTheTUI unlocks an encrypted vault in the TUI at a pseudo-terminal, which hands
// it to a vault process, locks it with 'qconnect vault lock' from a second process, and sees the TUI forget
// its own key within one tick.
func TestVaultLockElsewhereLocksTheTUI(t *testing.T) {
	if testing.Short() {
		t.Skip("the acceptance run builds the binary")
	}
	const passphrase = "canary-passphrase-6e13d0"
	dir := t.TempDir()
	bin := buildBinary(t, dir)
	configPath := filepath.Join(dir, "config.yaml")
	config := `version: 4
services:
  wiki:
    provider: bookstack
    base_url: https://example.invalid
credentials:
  vault-reader:
    type: vault
connections:
  wiki:
    service: wiki
    credential: vault-reader
defaults:
  connections:
    bookstack: wiki
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	offer := func(string) (string, error) { return passphrase, nil }
	if err := vault.New(dir).Set("vault-reader", "token-id", canaryPrimaryID, offer); err != nil {
		t.Fatalf("seed the vault: %v", err)
	}

	var seen strings.Builder
	c := &runner{bin: bin, seen: &seen, env: []string{
		"HOME=" + dir,
		"PATH=" + os.Getenv("PATH"),
		"TERM=xterm-256color",
		"QCONNECT_CONFIG=" + configPath,
		"QCONNECT_NO_UPDATE_CHECK=1",
		"XDG_RUNTIME_DIR=" + shortRuntimeDir(t),
		secret.StoreSelector + "=none",
	}}
	var processID int
	t.Cleanup(func() {
		_, _, _ = c.run(t, "vault", "lock")
		if processID == 0 {
			return
		}
		for deadline := time.Now().Add(5 * time.Second); processAlive(processID); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				killProcess(processID)
				break
			}
		}
		unregisterVaultProcess(processID)
	})
	statusPID := func() int {
		t.Helper()
		code, stdout, stderr := c.run(t, "vault", "status", "--output", "json")
		var doc struct {
			PID int64 `json:"pid"`
		}
		if code != 0 || json.Unmarshal([]byte(stdout), &doc) != nil {
			t.Fatalf("vault status: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		return int(doc.PID)
	}

	master, slave := openPTY(t)
	defer master.Close()
	size := struct{ rows, cols, x, y uint16 }{40, 130, 0, 0}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, slave.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&size))); errno != 0 {
		t.Fatalf("setting the terminal size: %v", errno)
	}
	cmd := exec.Command(bin, "tui")
	cmd.Env = c.environ(t)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the editor: %v", err)
	}
	_ = slave.Close()
	var mu sync.Mutex
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 8192)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			out.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	screen := func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(strings.Fields(ansiSequence.ReplaceAllString(out.String(), "")), " ")
	}
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	from := 0
	waitFor := func(marker string, limit time.Duration) {
		t.Helper()
		for deadline := time.Now().Add(limit); ; time.Sleep(20 * time.Millisecond) {
			now := screen()
			if i := strings.Index(now[from:], marker); i >= 0 {
				from += i + len(marker)
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("no %q appeared within %v; the screen reads: %q", marker, limit, now[max(from-200, 0):])
			}
		}
	}
	send := func(keys string) {
		t.Helper()
		if _, err := io.WriteString(master, keys); err != nil {
			t.Fatalf("typing at the terminal: %v", err)
		}
	}

	waitFor("Overview", 30*time.Second)
	waitFor("vault locked", 30*time.Second)
	send("\x0c")
	waitFor("Unlock vault", 30*time.Second)
	send(passphrase + "\r")
	waitFor("vault unlocked", 30*time.Second)
	// The hand-off to the vault process runs after the unlock; the window has seen the process once it is
	// done, and only then does a lock elsewhere count.
	for deadline := time.Now().Add(30 * time.Second); processID == 0; time.Sleep(50 * time.Millisecond) {
		if pid := statusPID(); pid > 0 {
			processID = pid
			registerVaultProcess(pid, bin)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the TUI started no vault process")
		}
	}
	// The process answers here a moment before the TUI's own hand-off has its answer.
	time.Sleep(time.Second)

	unlocked := from
	if code, stdout, stderr := c.run(t, "vault", "lock"); code != 0 {
		t.Fatalf("vault lock: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	// One tick is five seconds; the margin covers a loaded host. The terminal is only sent what changed, so
	// the status line arrives without the words it shares with the one before.
	waitFor("was locked outside this window", 15*time.Second)
	if !strings.Contains(screen()[unlocked:], "vault locked") {
		t.Errorf("the header does not read locked after the lock elsewhere: %q", screen()[unlocked:])
	}

	send("\x03")
	if err := cmd.Wait(); err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() > 130 {
			t.Fatalf("the editor ended with %v", err)
		}
	}
	finished = true
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	seen.WriteString(screen())
	if strings.Contains(seen.String(), passphrase) || strings.Contains(seen.String(), canaryPrimaryID) {
		t.Error("the passphrase or a stored secret reached the terminal")
	}
}
