//go:build (linux || darwin) && !legacy_vault_names

package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// A process that proves with another label, as one of another application or of the release before
// qatlas-connect does, is refused with its process id and is sent no request, the credential name included.
func TestClientRefusesAWrongProofLabel(t *testing.T) {
	for name, label := range map[string]string{"the earlier release": legacyProofLabel, "another label": "x"} {
		t.Run(name, func(t *testing.T) {
			tv := newTestVault(t)
			asked := false
			cfg := kitvaultproc.Config{Endpoint: endpointOf(t, tv.vault.Dir()), Version: Version, ProofLabel: label,
				IdleTimeout: time.Hour, Handlers: map[string]kitvaultproc.HandlerFunc{
					opGet: func(context.Context, *kitvaultproc.Held, json.RawMessage) (any, error) {
						asked = true
						return nil, nil
					}}}
			server, err := kitvaultproc.NewServer(cfg, kitvaultproc.State{Identity: tv.key}, false)
			if err != nil {
				t.Fatal(err)
			}
			l, err := cfg.Endpoint.ListenWith(localipc.ListenOptions{Refused: server.Refused})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { _ = server.Serve(l); close(done) }()
			t.Cleanup(func() { _ = server.Close(); <-done })

			_, _, err = tv.client(t).Get(withTimeout(t), "wiki", "token", tv.scope)
			var peer *PeerError
			if !errors.Is(err, ErrRefused) || !errors.As(err, &peer) || peer.PID != os.Getpid() {
				t.Fatalf("Get() = %v, want a refusal naming process %d", err, os.Getpid())
			}
			if asked {
				t.Fatal("the process was asked although it did not prove the vault key")
			}
		})
	}
}

// A vault process of another protocol version, one of a release before the kit's channel among them, cannot
// be asked to lock; Lock ends it with localipc's EndHolder, as another process of this user behind the
// vault's channel with its lock held. So is one that still answers replaced once the kit asked again.
func TestLockEndsAProcessThatCannotBeAsked(t *testing.T) {
	for name, line := range map[string]string{
		"protocol version 10": `{"v":10,"error":"version"}` + "\n",
		"another version":     `{"v":1,"app":3,"error":"version"}` + "\n",
		"replaced": fmt.Sprintf(`{"v":%d,"app":%d,"error":"replaced"}`, kitvaultproc.ProtocolVersion, Version) +
			"\n",
	} {
		t.Run(name, func(t *testing.T) {
			tv := newTestVault(t)
			h := startHelper(t, helperSpec{Mode: "serve", VaultDir: tv.vault.Dir(), Line: line}, "listening")
			if name == "replaced" { // temporary macOS diagnosis
				macDiagReplaced(t, endpointOf(t, tv.vault.Dir()), h.pid(), Endpoint, func(dir string) int {
					return startHelper(t, helperSpec{Mode: "serve", VaultDir: dir, Line: line}, "listening").pid()
				})
			}
			client := tv.client(t)
			if _, err := client.Status(withTimeout(t)); err == nil {
				t.Fatal("Status() succeeded")
			}
			pid, err := EndReplaced(withTimeout(t), client, nil)
			if err != nil || pid != h.pid() {
				t.Fatalf("EndReplaced() = %d, %v, want the helper %d ended", pid, err, h.pid())
			}
			if got := h.line(t); got != "locked" {
				t.Fatalf("the helper said %q, want it ended", got)
			}
		})
	}
}

// What answers with another version but is no vault process of this user behind the vault's channel is not
// ended: a socket without its lock, and this very process. Lock reports why it could not lock instead.
func TestLockEndsNothingThatFailsTheCheck(t *testing.T) {
	t.Run("socket without its lock", func(t *testing.T) {
		tv := newTestVault(t)
		socket, err := SocketPath(tv.vault.Dir())
		if err != nil {
			t.Fatal(err)
		}
		l := listenRaw(t, socket)
		if err := os.Chmod(socket, 0o600); err != nil {
			t.Fatal(err)
		}
		go answerEvery(l, `{"v":10,"error":"version"}`+"\n")
		if err := tv.client(t).Lock(withTimeout(t)); !errors.Is(err, ErrVersion) {
			t.Fatalf("Lock() = %v, want ErrVersion", err)
		}
		if _, err := os.Lstat(socket); err != nil {
			t.Fatalf("the socket is gone: %v", err)
		}
	})
	t.Run("this process", func(t *testing.T) {
		tv := newTestVault(t)
		l, err := endpointOf(t, tv.vault.Dir()).Listen()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		go answerEvery(l, `{"v":10,"error":"version"}`+"\n")
		err = tv.client(t).Lock(withTimeout(t))
		if !errors.Is(err, ErrVersion) || !strings.Contains(err.Error(), "process") {
			t.Fatalf("Lock() = %v, want ErrVersion naming the process", err)
		}
	})
}

// A refusal of the kit is told with the text Connect always had, with the process that refused, and still
// matches the kit's error.
func TestClientTellsRefusalsAsBefore(t *testing.T) {
	answer := func(code string) string {
		return fmt.Sprintf(`{"v":%d,"app":%d,"error":%q}`, kitvaultproc.ProtocolVersion, Version, code) + "\n"
	}
	for name, tc := range map[string]struct {
		line string
		want string
		is   []error
	}{
		"refused": {answer("refused"), ErrProgramRefused.Error(), []error{ErrProgramRefused, ErrRefused,
			kitvaultproc.ErrProgramRefused}},
		"version":          {`{"v":10,"error":"version"}` + "\n", ErrVersion.Error(), []error{ErrVersion, kitvaultproc.ErrVersion}},
		"challenge":        {answer("challenge"), errUnproven.Error(), []error{ErrRefused, kitvaultproc.ErrUnproven}},
		"replaced":         {answer("replaced"), ErrReplaced.Error(), []error{ErrReplaced, ErrProgramRefused, kitvaultproc.ErrReplaced}},
		"program-mismatch": {answer("program-mismatch"), ErrHandover.Error() + ": the program installed is not the one the release holds", []error{ErrHandover, kitvaultproc.ErrHandover}},
		"handover-unsupported": {answer("handover-unsupported"), ErrHandover.Error() + ": no successor took the vault over",
			[]error{ErrHandover}},
		"release-unverified": {answer("release-unverified"), ErrHandover.Error() + ": it could not verify the release",
			[]error{ErrHandover}},
		"handover-too-large": {answer("handover-too-large"), ErrHandover.Error() + ": it could not verify the release",
			[]error{ErrHandover}},
		"handover-busy": {answer("handover-busy"), ErrHandover.Error() + ": no successor took the vault over",
			[]error{ErrHandover}},
		"handover-bad-request": {answer("handover-bad-request"), errInvalidRequest.Error(),
			[]error{errInvalidRequest, kitvaultproc.ErrBadRequest}},
	} {
		t.Run(name, func(t *testing.T) {
			tv := newTestVault(t)
			h := startHelper(t, helperSpec{Mode: "serve", VaultDir: tv.vault.Dir(), Line: tc.line}, "listening")
			_, err := tv.client(t).Status(withTimeout(t))
			want := fmt.Sprintf("%s (process %d)", tc.want, h.pid())
			if err == nil || err.Error() != want {
				t.Fatalf("Status() = %v, want %q", err, want)
			}
			for _, target := range tc.is {
				if !errors.Is(err, target) {
					t.Errorf("Status() = %v, does not match %v", err, target)
				}
			}
		})
	}
	tv := newTestVault(t)
	if _, err := (&Client{Endpoint: endpointOf(t, tv.vault.Dir())}).Status(withTimeout(t)); err != ErrNoRecipient {
		t.Errorf("Status() without a recipient = %v, want ErrNoRecipient", err)
	}
}

// A vault process that does not start is told as before: the reason it reported, or that it ended first.
func TestStartProcessTellsAFailedStartAsBefore(t *testing.T) {
	for name, tc := range map[string]struct {
		spec helperSpec
		want string
	}{
		"reported": {helperSpec{Mode: "fail", Line: "the vault key handed over cannot be read"},
			"the vault process did not start: the vault key handed over cannot be read"},
		"ended": {helperSpec{Mode: "exit"}, "the vault process ended before it was ready"},
	} {
		t.Run(name, func(t *testing.T) {
			tv := newTestVault(t)
			raw, _ := json.Marshal(tc.spec)
			t.Setenv(helperEnv, string(raw))
			_, err := StartProcess(withTimeout(t), tv.dir+"/config.yaml", tv.snap, tv.client(t))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("StartProcess() = %v, want %q", err, tc.want)
			}
		})
	}
}

// A process of the previous protocol version and a client of this one refuse each other, in both
// directions, with ErrVersion.
func TestProtocolVersionMismatchIsReported(t *testing.T) {
	if Version != 11 {
		t.Fatalf("Version = %d, want 11", Version)
	}
	tv := newTestVault(t)
	ctx := withTimeout(t)
	cfg := kitvaultproc.Config{Endpoint: endpointOf(t, tv.vault.Dir()), Version: Version - 1, ProofLabel: proofLabel,
		IdleTimeout: time.Hour}
	previous, err := kitvaultproc.NewServer(cfg, kitvaultproc.State{Identity: tv.key}, false)
	if err != nil {
		t.Fatal(err)
	}
	l, err := cfg.Endpoint.ListenWith(localipc.ListenOptions{Refused: previous.Refused})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = previous.Serve(l); close(done) }()
	if _, err := tv.client(t).Status(ctx); !errors.Is(err, ErrVersion) {
		t.Errorf("Status() of a process of version %d = %v, want ErrVersion", Version-1, err)
	}
	_ = previous.Close()
	<-done

	_, client := tv.serve(t, ServerOptions{})
	old := client.kit()
	old.Version = Version - 1
	if _, err := old.Status(ctx); !errors.Is(err, kitvaultproc.ErrVersion) {
		t.Errorf("Status() of a client of version %d = %v, want ErrVersion", Version-1, err)
	}
}

// As before the kit, the vault process keeps its socket below a runtime directory others may read but not
// write, and refuses one they may write, which would let them replace the directory below it.
func TestRuntimeDirectoryOthersMayReadButNotWrite(t *testing.T) {
	tv := newTestVault(t)
	root := os.Getenv("XDG_RUNTIME_DIR")
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	_, client := tv.serve(t, ServerOptions{})
	if _, err := client.Status(withTimeout(t)); err != nil {
		t.Fatalf("Status() below a readable runtime directory = %v", err)
	}
	if path := client.Path(); !strings.HasPrefix(path, filepath.Join(root, runtimeDirName)+string(filepath.Separator)) {
		t.Fatalf("the socket is %s, want it below %s", path, root)
	}

	writable := shortTempDir(t)
	if err := os.Chmod(writable, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", writable)
	if path, err := SocketPath(tv.vault.Dir()); !errors.Is(err, ErrRefused) {
		t.Fatalf("SocketPath() below a runtime directory others may write = %q, %v, want ErrRefused", path, err)
	}
}
