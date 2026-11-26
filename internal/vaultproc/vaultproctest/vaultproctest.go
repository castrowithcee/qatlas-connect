// Package vaultproctest stands in for vault processes that refuse or answer otherwise than this release's in
// tests: a channel of the vault with its lock that answers every connection with one fixed line.
package vaultproctest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
	"github.com/castrowithcee/qatlas-kit/localipc"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// Answer is the first answer of a vault process of this release that refuses a client with code, such as
// "refused" from a process whose program an update replaced, or "replaced" from one that hands over.
func Answer(code string) string {
	return fmt.Sprintf(`{"v":%d,"app":%d,"error":%q}`, kitvaultproc.ProtocolVersion, vaultproc.Version, code) + "\n"
}

// Serve answers every connection l accepts with line, after the client's first line, until l is closed.
func Serve(l net.Listener, line string) {
	for {
		conn, err := l.Accept()
		var refused *localipc.PeerError
		if errors.As(err, &refused) {
			continue // a peer the listener refused and closed, such as one that left before it was checked
		}
		if err != nil {
			return
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = io.WriteString(conn, line)
		_ = conn.Close()
	}
}

// Reply listens at the channel of the vault process for vaultDir, with its lock, and answers every client
// with line from the test's own process until the test ends.
func Reply(t testing.TB, vaultDir, line string) {
	t.Helper()
	e, err := vaultproc.Endpoint(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.Listen()
	if err != nil {
		t.Fatalf("Listen() = %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go Serve(l, line)
}

// Refuse is Reply for a vault process whose program an update replaced, which refuses every client.
func Refuse(t testing.TB, vaultDir string) { Reply(t, vaultDir, Answer("refused")) }

// helperEnv makes a test binary the stand-in of Start: the JSON of a helper.
const helperEnv = "QCONNECT_VAULTPROCTEST_HELPER"

type helper struct {
	VaultDir   string `json:"vault_dir"`
	RuntimeDir string `json:"runtime_dir,omitempty"`
	Line       string `json:"line"`
}

// RunIfHelper serves as the stand-in Start runs and exits, when this test binary was started as one. A
// TestMain of a package that uses Start calls it first.
func RunIfHelper() {
	raw := os.Getenv(helperEnv)
	if raw == "" {
		return
	}
	var h helper
	if json.Unmarshal([]byte(raw), &h) != nil {
		os.Exit(2)
	}
	e, err := vaultproc.Endpoint(h.VaultDir)
	if err != nil {
		os.Exit(1)
	}
	if h.RuntimeDir != "" {
		e.RuntimeDirName = h.RuntimeDir
	}
	l, err := e.Listen()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// The stand-in ends with the test binary, which holds its standard input.
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = l.Close()
		os.Exit(0)
	}()
	fmt.Println("ready")
	Serve(l, h.Line)
	os.Exit(0)
}

// Start runs this test binary as a process of its own that listens at the channel of the vault process for
// vaultDir, below the runtime directory name runtimeDir (empty for this release's), with its lock, and
// answers every client with line. It returns once the process listens; the process is killed on cleanup
// should it still run. A stand-in in a process of its own is one localipc's EndHolder may end.
func Start(t testing.TB, vaultDir, runtimeDir, line string) *Process {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := json.Marshal(helper{VaultDir: vaultDir, RuntimeDir: runtimeDir, Line: line})
	cmd := exec.Command(program)
	cmd.Env = append(os.Environ(), helperEnv+"="+string(spec))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	text, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(text) != "ready" {
		t.Fatalf("the vault process stand-in did not start: %q", text)
	}
	return &Process{PID: cmd.Process.Pid, done: done}
}

// Process is a stand-in Start runs.
type Process struct {
	PID  int
	done <-chan struct{}
}

// Ended reports whether the process ended within timeout.
func (p *Process) Ended(timeout time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(timeout):
		return false
	}
}
