//go:build linux

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/castrowithcee/qatlas-connect/internal/secret"
)

var ansiSequence = regexp.MustCompile("\x1b\\[[0-9;?<>=]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)|\x1b[()][A-Za-z0-9]|\x1b[=>]")

// TestToolRequestDecidedInTheTUI files a request over the CLI, accepts it in the TUI at a pseudo-terminal
// and sees the tool offered by 'qconnect tools' afterwards.
func TestToolRequestDecidedInTheTUI(t *testing.T) {
	if testing.Short() {
		t.Skip("the acceptance run builds the binary")
	}
	dir := t.TempDir()
	bin := buildBinary(t, dir)
	configPath := filepath.Join(dir, "config.yaml")
	config := `version: 4
services:
  wiki:
    provider: bookstack
    base_url: https://example.invalid
credentials:
  reader:
    type: env
    values:
      token-id: E2E_REQ_ID
      token-secret: E2E_REQ_SECRET
connections:
  primary:
    id: 0a1b2c3d4e5f60718293a4b5c6d7e8f9
    service: wiki
    credential: reader
    effects: [read]
defaults:
  connections:
    bookstack: primary
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	c := &runner{bin: bin, seen: &seen, env: []string{
		"HOME=" + dir,
		"PATH=" + os.Getenv("PATH"),
		"TERM=xterm-256color",
		"QCONNECT_CONFIG=" + configPath,
		"QCONNECT_NO_UPDATE_CHECK=1",
		secret.StoreSelector + "=none",
		"E2E_REQ_ID=" + canaryPrimaryID,
		"E2E_REQ_SECRET=" + canaryPrimarySecret,
	}}
	const tool = "bookstack.pages.delete"
	offered := func() bool {
		code, stdout, stderr := c.run(t, "tools", "bookstack", "content", "--output", "json")
		if code != 0 {
			t.Fatalf("tools exit=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		return strings.Contains(stdout, `"id":"`+tool+`"`)
	}
	if offered() {
		t.Fatalf("%s is offered before anyone accepted it", tool)
	}
	if code, stdout, stderr := c.run(t, "request", tool); code != 0 || !strings.Contains(stdout, "status: open") {
		t.Fatalf("request exit=%d stdout=%q stderr=%q", code, stdout, stderr)
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
	// waitFor waits until marker shows after what the screen held when the last key went in.
	from := 0
	waitFor := func(marker string) {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			now := screen()
			if i := strings.Index(now[from:], marker); i >= 0 {
				from += i + len(marker)
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("no %q appeared; the screen reads: %q", marker, now[max(from-200, 0):])
			}
		}
	}
	send := func(keys string) {
		t.Helper()
		if _, err := io.WriteString(master, keys); err != nil {
			t.Fatalf("typing at the terminal: %v", err)
		}
	}

	waitFor("Overview")
	waitFor("open tool requests (1)")
	// Access, then the Approvals part: the digit of the area, the way back to the tab line, one tab right,
	// down into the list.
	send("5")
	waitFor("Tokens")
	send("\x1b[Z")
	send("\x1b[C")
	send("\x1b[B")
	waitFor("tools: 1")
	waitFor("expires in 14d")
	send("\r")
	waitFor("Tool request for primary")
	waitFor("[x] " + tool)
	send("y")
	// The tool needs its exact ID in the allow-list, so only switching to a tools list is offered.
	waitFor("Switch to a tools list")
	waitFor("Not possible: " + tool)
	send("\r")
	waitFor("Accepted 1 tool(s) for primary")
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

	if !offered() {
		t.Fatalf("%s is not offered by 'qconnect tools' after accepting it", tool)
	}
	if data, err := os.ReadFile(configPath); err != nil || !strings.Contains(string(data), tool) {
		t.Fatalf("config = %q, %v", data, err)
	}
}
