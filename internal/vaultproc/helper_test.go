//go:build linux || darwin || windows

package vaultproc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"
)

// helperEnv makes the test binary a helper process instead of running the tests; its value is the JSON of a
// helperSpec.
const helperEnv = "QCONNECT_VAULTPROC_TEST_HELPER"

// helperSpec is what a helper process does.
//
//   - serve: listen at the channel of VaultDir below RuntimeDir (this release's when empty), with its lock,
//     say "listening", answer every connection with Line, and on SIGTERM say "locked" and end.
//   - successor: run 'qconnect vault serve --successor' for ConfigPath (see Serve), trusting the release key
//     TrustedKey, serving ToConfig where it is set.
//   - exit: end at once, as a vault process that ends before it reports.
//   - fail: report Line as the reason a vault process did not start.
type helperSpec struct {
	Mode       string `json:"mode"`
	VaultDir   string `json:"vault_dir,omitempty"`
	RuntimeDir string `json:"runtime_dir,omitempty"`
	Line       string `json:"line,omitempty"`
	ConfigPath string `json:"config_path,omitempty"`
	TrustedKey string `json:"trusted_key,omitempty"`
	// ToConfig, for successor, is the configuration the successor serves instead of ConfigPath, as after a
	// migration.
	ToConfig string `json:"to_config,omitempty"`
}

func TestMain(m *testing.M) {
	vault.LowerWorkFactorForTests()
	if raw := os.Getenv(helperEnv); raw != "" {
		var spec helperSpec
		if json.Unmarshal([]byte(raw), &spec) != nil {
			os.Exit(2)
		}
		os.Exit(runHelper(spec))
	}
	os.Exit(m.Run())
}

func runHelper(spec helperSpec) int {
	switch spec.Mode {
	case "serve":
		// It ends with the test binary, which holds its standard input.
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			os.Exit(0)
		}()
		e, err := Endpoint(spec.VaultDir)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		if spec.RuntimeDir != "" {
			e.RuntimeDirName = spec.RuntimeDir
		}
		l, err := e.Listen()
		if err != nil {
			fmt.Println(err)
			return 1
		}
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		go func() {
			<-signals
			fmt.Println("locked")
			_ = l.Close()
			os.Exit(0)
		}()
		fmt.Println("listening")
		answerEvery(l, spec.Line)
		select {}
	case "successor":
		return runSuccessorHelper(spec)
	case "exit":
		// Taking the handover first keeps the starter's write from failing, so the start fails because the
		// process ended and not because it did not take the vault.
		if child, ok := background.Inherit(); ok {
			_, _ = child.ReadHandover(64 << 20)
		}
		return 0
	case "fail":
		child, ok := background.Inherit()
		if !ok {
			return 2
		}
		child.Fail(spec.Line)
		return 1
	}
	return 2
}

// answerEvery answers every connection l accepts with line, after the client's first line.
func answerEvery(l net.Listener, line string) {
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

// helperProcess is a helper process a test started.
type helperProcess struct {
	cmd   *exec.Cmd
	lines chan string
	done  chan struct{}
}

// startHelper starts the test binary as a helper process for spec and returns once it said its first line,
// which must be want. It is ended on cleanup should it still run.
func startHelper(t *testing.T, spec helperSpec, want string) *helperProcess {
	t.Helper()
	raw, _ := json.Marshal(spec)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"="+string(raw))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h := &helperProcess{cmd: cmd, lines: make(chan string, 16), done: make(chan struct{})}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			h.lines <- strings.TrimSpace(scanner.Text())
		}
		close(h.lines)
		_ = cmd.Wait()
		close(h.done)
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-h.done
		}
	})
	if first := h.line(t); first != want {
		t.Fatalf("the helper process said %q, want %q", first, want)
	}
	return h
}

// line returns the next line the helper said.
func (h *helperProcess) line(t *testing.T) string {
	t.Helper()
	select {
	case text, ok := <-h.lines:
		if !ok {
			return "(ended)"
		}
		return text
	case <-time.After(10 * time.Second):
		return "(nothing)"
	}
}

// ended reports whether the helper ended within timeout.
func (h *helperProcess) ended(timeout time.Duration) bool {
	select {
	case <-h.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// pid is the helper's process id.
func (h *helperProcess) pid() int { return h.cmd.Process.Pid }

// shortTempDir returns a fresh directory short enough for a socket path below it, removed after the test.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS == "darwin" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "qp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// testVault is an encrypted vault for the tests, unlocked, holding wiki's token for the connection wiki,
// approved, in a configuration directory of its own with a runtime directory of the test's own.
type testVault struct {
	dir   string // the configuration directory
	vault *vault.Vault
	key   *age.X25519Identity
	snap  vault.Snapshot
	scope vault.Scope
}

const (
	testPassphrase = "vaultproc-test-phrase"
	testSecret     = "cnry-vp-4f1"
)

func newTestVault(t *testing.T) *testVault {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))
	dir := filepath.Join(t.TempDir(), "connect")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("version: 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := vault.New(dir)
	offer := func(string) (string, error) { return testPassphrase, nil }
	if err := v.Set("wiki", "token", testSecret, offer); err != nil {
		t.Fatal(err)
	}
	v = vault.New(dir)
	if _, err := v.Unlock(testPassphrase); err != nil {
		t.Fatal(err)
	}
	scope := vault.Scope{Connection: "wiki", Credential: "wiki", Provider: "bookstack", Origin: "https://wiki.example",
		Effects: []string{"read"}}
	if err := v.Approve([]vault.Scope{scope}); err != nil {
		t.Fatal(err)
	}
	snap, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	key, err := age.ParseX25519Identity(snap.Identity)
	if err != nil {
		t.Fatal(err)
	}
	return &testVault{dir: dir, vault: v, key: key, snap: snap, scope: scope}
}

// serve serves tv from this test process with opts and returns the server and its client.
func (tv *testVault) serve(t *testing.T, opts ServerOptions) (*Server, *Client) {
	t.Helper()
	server, err := NewServer(tv.vault.Dir(), tv.snap, opts)
	if err != nil {
		t.Fatalf("NewServer() = %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, tv.client(t)
}

// client is the client of tv's vault process.
func (tv *testVault) client(t *testing.T) *Client {
	t.Helper()
	client, err := ProcessClientOf(tv.vault)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// listenRaw listens at path without a lock, in a private directory, as something that is no vault process.
func listenRaw(t *testing.T, path string) net.Listener {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// endpointOf returns the endpoint of vaultDir, failing the test otherwise.
func endpointOf(t *testing.T, vaultDir string) localipc.Endpoint {
	t.Helper()
	e, err := Endpoint(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// withTimeout is a context for one test step.
func withTimeout(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
