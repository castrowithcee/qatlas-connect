//go:build linux || darwin

package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/approval"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	qconfig "github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-connect/internal/provider/bookstack"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

// runAtPTY runs the binary with a pseudo-terminal as its terminal. With a passphrase it waits for the
// prompt, echo off, and types it; without one it only expects the run to end. It returns what the run wrote
// to the terminal and its exit code.
func runAtPTY(t *testing.T, c *runner, passphrase string, args ...string) (string, int) {
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
				return
			}
		}
	}()
	text := func() string { mu.Lock(); defer mu.Unlock(); return out.String() }

	if passphrase != "" {
		for deadline := time.Now().Add(10 * time.Second); !strings.Contains(text(), "passphrase: ") || !echoOff(master); {
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				t.Fatalf("no passphrase prompt with echo off appeared: %q", text())
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := io.WriteString(master, passphrase+"\n"); err != nil {
			t.Fatalf("typing at the terminal: %v", err)
		}
	}
	err := cmd.Wait()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	c.seen.WriteString(text())
	return text(), code
}

type e2eRequestList struct {
	Requests []struct {
		ID         string `json:"id"`
		Connection string `json:"connection"`
		Reason     string `json:"reason"`
		Groups     []struct {
			Group string   `json:"group"`
			Title string   `json:"title"`
			Tools []string `json:"tools"`
		} `json:"groups"`
	} `json:"requests"`
}

// TestRequestsEndToEnd decides tool requests at a pseudo-terminal: with an encrypted vault the passphrase is
// asked, a wrong one changes nothing, and without a terminal every decision is admin-required.
func TestRequestsEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("the acceptance run builds the binary")
	}
	const passphrase = "synthetic-passphrase-7c41d2"

	dir := t.TempDir()
	bin := buildBinary(t, dir)
	configPath := filepath.Join(dir, "config.yaml")
	config := `version: 4
services:
  wiki:
    provider: bookstack
    base_url: https://wiki.example.test
credentials:
  vault-reader:
    type: vault
  key-reader:
    type: keyring
connections:
  effects:
    id: 0123456789abcdef0123456789abcdef
    service: wiki
    credential: vault-reader
    effects: [read]
  listed:
    id: fedcba9876543210fedcba9876543210
    service: wiki
    credential: key-reader
    tools: [bookstack.pages.list]
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	v := vault.New(dir)
	offer := func(string) (string, error) { return passphrase, nil }
	if err := v.Set("vault-reader", "token-id", "synthetic-id-1", offer); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("vault-reader", "token-secret", "synthetic-value-2", offer); err != nil {
		t.Fatal(err)
	}
	reg := capability.NewRegistry()
	if err := bookstack.Register(reg); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	c := &runner{bin: bin, seen: &seen, env: []string{
		"HOME=" + dir,
		"PATH=" + os.Getenv("PATH"),
		"QCONNECT_CONFIG=" + configPath,
		"XDG_RUNTIME_DIR=" + shortRuntimeDir(t),
		secret.StoreSelector + "=none",
	}}
	cfg, err := qconfig.Load(configPath, reg)
	if err != nil {
		t.Fatal(err)
	}
	if approved, _, err := approval.Approve(t.Context(), cfg, v, nil); err != nil || len(approved) != 1 {
		t.Fatalf("approving: %v, %v", approved, err)
	}
	src, err := manage.NewRequestSource(configPath, reg)
	if err != nil {
		t.Fatal(err)
	}
	env := accessrequest.Env{Config: cfg, Tools: src.Tools, Now: time.Now()}
	add := func(name, reason string, tools ...string) {
		t.Helper()
		_, err := src.Queue.Add(env, accessrequest.Input{
			Connection: name, ConnectionID: cfg.Connections[name].ID, Tools: tools, Reason: reason, Project: "synthetic-project",
		})
		if err != nil {
			t.Fatalf("adding the request of %s: %v", name, err)
		}
	}
	add("effects", "need \x1b[31mto edit pages\x1b[0m", "bookstack.pages.update", "bookstack.comments.create")
	add("listed", "", "bookstack.pages.update", "bookstack.pages.create", "bookstack.books.update")

	list := func() e2eRequestList {
		t.Helper()
		code, stdout, stderr := c.run(t, "requests", "--output", "json")
		if code != 0 {
			t.Fatalf("requests: exit %d, stderr %q", code, stderr)
		}
		var l e2eRequestList
		if err := json.Unmarshal([]byte(stdout), &l); err != nil {
			t.Fatalf("requests output %q: %v", stdout, err)
		}
		return l
	}
	idOf := func(name string) string {
		t.Helper()
		for _, r := range list().Requests {
			if r.Connection == name {
				return r.ID
			}
		}
		t.Fatalf("no open request of %s", name)
		return ""
	}
	files := func() string {
		cfgData, _ := os.ReadFile(configPath)
		queueData, _ := os.ReadFile(src.Queue.Path())
		return string(cfgData) + "\x00" + string(queueData)
	}

	t.Run("the list needs no terminal, groups the tools and cleans the reason", func(t *testing.T) {
		l := list()
		if len(l.Requests) != 2 {
			t.Fatalf("requests = %+v, want two", l.Requests)
		}
		for _, r := range l.Requests {
			if strings.ContainsRune(r.Reason, 0x1b) {
				t.Errorf("reason %q carries a control character", r.Reason)
			}
			if r.Connection == "effects" && (len(r.Groups) != 2 || r.Groups[0].Group == "" || r.Groups[0].Title == "") {
				t.Errorf("groups of effects = %+v, want two titled groups", r.Groups)
			}
		}
		code, stdout, _ := c.run(t, "requests")
		if code != 0 || !strings.Contains(stdout, "bookstack.pages.update") {
			t.Errorf("toon list: exit %d, stdout %q", code, stdout)
		}
	})

	t.Run("a decision without a terminal is admin-required and changes nothing", func(t *testing.T) {
		before := files()
		id := idOf("listed")
		for _, args := range [][]string{
			{"requests", "approve", id},
			{"requests", "decline", id},
			{"requests", "block", id},
			{"requests", "approve", id, "--as", "tools"},
		} {
			code, stdout, stderr := c.run(t, args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, "admin-required") {
				t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
			}
		}
		if files() != before {
			t.Error("a refused decision changed a file")
		}
	})

	t.Run("a wrong passphrase has no effect", func(t *testing.T) {
		before := files()
		out, code := runAtPTY(t, c, "wrong-"+passphrase, "requests", "approve", idOf("listed"))
		if code == 0 || strings.Contains(out, passphrase) {
			t.Errorf("wrong passphrase: exit %d, output %q", code, out)
		}
		if files() != before {
			t.Error("a wrong passphrase changed a file")
		}
	})

	t.Run("approving some tools of a tools list leaves the rest open", func(t *testing.T) {
		id := idOf("listed")
		out, code := runAtPTY(t, c, passphrase, "requests", "approve", id, "--tool", "bookstack.pages.update",
			"--output", "json")
		if code != 0 || !strings.Contains(out, `"result":"accepted"`) || strings.Contains(out, passphrase) {
			t.Fatalf("approve: exit %d, output %q", code, out)
		}
		data, _ := os.ReadFile(configPath)
		if !strings.Contains(string(data), "bookstack.pages.update") {
			t.Errorf("the tool was not added:\n%s", data)
		}
		for _, r := range list().Requests {
			if r.Connection != "listed" {
				continue
			}
			var open []string
			for _, g := range r.Groups {
				open = append(open, g.Tools...)
			}
			if strings.Join(open, " ") != "bookstack.books.update bookstack.pages.create" {
				t.Errorf("tools left open = %v", open)
			}
		}
	})

	t.Run("--as is refused for a tools list, before the passphrase", func(t *testing.T) {
		before := files()
		out, code := runAtPTY(t, c, "", "requests", "approve", idOf("listed"), "--as", "effect")
		if code != 2 || !strings.Contains(out, "invalid-request") || files() != before {
			t.Errorf("exit %d, output %q", code, out)
		}
	})

	t.Run("a connection by effect needs --as, which names both ways", func(t *testing.T) {
		before := files()
		out, code := runAtPTY(t, c, "", "requests", "approve", idOf("effects"))
		for _, want := range []string{"--as effect adds", "--as tools switches", "invalid-request"} {
			if code != 2 || !strings.Contains(out, want) {
				t.Errorf("exit %d, output %q, want %q", code, out, want)
			}
		}
		if files() != before {
			t.Error("a refused decision changed a file")
		}
	})

	t.Run("approving by effect with --as tools saves the list and approves the connection", func(t *testing.T) {
		id := idOf("effects")
		out, code := runAtPTY(t, c, passphrase, "requests", "approve", id, "--as", "tools",
			"--tool", "bookstack.pages.update", "--output", "json")
		if code != 0 || !strings.Contains(out, `"state":"approved"`) {
			t.Fatalf("approve: exit %d, output %q", code, out)
		}
		data, _ := os.ReadFile(configPath)
		if !strings.Contains(string(data), "tools:") || !strings.Contains(string(data), "bookstack.pages.update") {
			t.Errorf("the connection was not switched to a list:\n%s", data)
		}
		out, code = runAtPTY(t, c, passphrase, "vault", "approve")
		if code != 0 || !strings.Contains(out, "nothing is open") {
			t.Errorf("vault approve: exit %d, output %q, want nothing open", code, out)
		}
	})

	t.Run("decline closes the tools and changes nothing else", func(t *testing.T) {
		cfgBefore, _ := os.ReadFile(configPath)
		id := idOf("effects")
		out, code := runAtPTY(t, c, passphrase, "requests", "decline", id, "--output", "json")
		if code != 0 || !strings.Contains(out, `"result":"declined"`) {
			t.Fatalf("decline: exit %d, output %q", code, out)
		}
		cfgAfter, _ := os.ReadFile(configPath)
		if string(cfgBefore) != string(cfgAfter) {
			t.Error("declining changed the configuration")
		}
		for _, r := range list().Requests {
			if r.Connection == "effects" {
				t.Errorf("the request of effects is still open: %+v", r)
			}
		}
	})

	t.Run("block closes the tools and records them as blocked", func(t *testing.T) {
		id := idOf("listed")
		out, code := runAtPTY(t, c, passphrase, "requests", "block", id, "--output", "json")
		if code != 0 || !strings.Contains(out, `"result":"blocked"`) {
			t.Fatalf("block: exit %d, output %q", code, out)
		}
		data, _ := os.ReadFile(configPath)
		if !strings.Contains(string(data), "blocked_tools:") || !strings.Contains(string(data), "bookstack.pages.create") {
			t.Errorf("the tools were not recorded as blocked:\n%s", data)
		}
		if got := list().Requests; len(got) != 0 {
			t.Errorf("requests = %+v, want none open", got)
		}
	})

	if strings.Contains(seen.String(), passphrase) {
		t.Error("the passphrase reached the output")
	}
}
