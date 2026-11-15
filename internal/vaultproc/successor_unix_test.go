//go:build (linux || darwin) && !legacy_vault_names

package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
)

// runSuccessorHelper is the helper process of a successor: 'qconnect vault serve --successor'.
func runSuccessorHelper(spec helperSpec) int {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(spec.TrustedKey))
	if err != nil {
		return 2
	}
	if err := Serve(context.Background(), ServeOptions{ConfigPath: spec.ConfigPath, Successor: true,
		TrustedKeys: []ssh.PublicKey{key}}); err != nil {
		return 1
	}
	return 0
}

// earlierHandover is what a vault process of a release before the kit's channel, up to protocol version 9,
// hands its successor: the JSON of vault.Snapshot, with the time it would have locked at.
func earlierHandover(t *testing.T, tv *testVault, locksAt, unlockedAt time.Time) []byte {
	t.Helper()
	data, err := json.Marshal(vault.Snapshot{Identity: tv.snap.Identity, Secrets: tv.snap.Secrets,
		Bindings: tv.snap.Bindings, LocksAt: &locksAt, UnlockedAt: &unlockedAt})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// startSuccessor starts the test binary as 'qconnect vault serve --successor' for tv, as a vault process of
// an earlier release starts it, handing it data, and returns its process id once it reported ready. It is
// killed on cleanup should it still run.
func startSuccessor(t *testing.T, tv *testVault, data []byte) int {
	t.Helper()
	signer := releasetest.NewSigner(t)
	spec, _ := json.Marshal(helperSpec{Mode: "successor", ConfigPath: tv.dir + "/config.yaml",
		TrustedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))})
	result, err := background.Start(withTimeout(t), background.Command{Program: os.Args[0],
		Env: append(os.Environ(), helperEnv+"="+string(spec))}, data, background.Options{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("starting the successor: %v", err)
	}
	t.Cleanup(func() {
		_ = result.Process.Kill()
		select {
		case <-result.Process.Done():
		case <-time.After(5 * time.Second):
		}
	})
	if result.Report != background.ReportReady {
		t.Fatalf("the successor reported %q", result.Report)
	}
	return result.PID
}

// moveBeside does what a listener of a release before the kit's channel does in its handover: it moves the
// successor's lock and then its socket, both beside socket at socket.next, onto socket.
func moveBeside(t *testing.T, socket string) {
	t.Helper()
	for _, suffix := range []string{".lock", ""} {
		if err := os.Rename(socket+".next"+suffix, socket+suffix); err != nil {
			t.Fatalf("moving the successor's %q: %v", suffix, err)
		}
	}
}

// A successor started by a vault process of a release before the kit's channel, v2.2.0 or one after the
// rename alike, reads that release's handover format and takes the vault over, with the time that process
// would have locked at, from the channel that process serves: this release's own, after the rename, or the
// earlier release's, before it. It serves nothing before that process handed over.
func TestSuccessorTakesTheVaultOverFromAnEarlierFormat(t *testing.T) {
	for name, earlierNames := range map[string]bool{"after the rename": false, "v2.2.0": true} {
		t.Run(name, func(t *testing.T) {
			tv := newTestVault(t)
			served := endpointOf(t, tv.vault.Dir())
			if earlierNames {
				var ok bool
				if served, ok = earlierEndpoint(tv.vault.Dir()); !ok {
					t.Fatal("no channel of the earlier release")
				}
			}
			// The predecessor's channel, with its lock, as its listener holds it.
			predecessor, err := served.Listen()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = predecessor.Close() })
			socket, err := served.Path()
			if err != nil {
				t.Fatal(err)
			}

			locksAt := time.Now().Add(time.Hour).Truncate(time.Second)
			pid := startSuccessor(t, tv, earlierHandover(t, tv, locksAt, time.Now().Add(-time.Minute)))
			client := tv.client(t)
			ctx := withTimeout(t)
			early := *client
			early.Timeout = 300 * time.Millisecond
			if _, err := early.Status(ctx); err == nil {
				t.Fatal("the successor answered before the handover")
			}

			moveBeside(t, socket)
			var status Status
			for deadline := time.Now().Add(10 * time.Second); ; {
				if status, err = client.Status(ctx); err == nil || time.Now().After(deadline) {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err != nil || status.PID != pid || !status.LocksAt.Equal(locksAt) {
				t.Fatalf("Status() after the handover = %+v, %v, want process %d locking at %v", status, err, pid,
					locksAt)
			}
			if status.Update != vault.UpdateHandover {
				t.Errorf("the successor reports the update setting %q, want handover", status.Update)
			}
			value, found, err := client.Get(ctx, "wiki", "token", tv.scope)
			if err != nil || !found || value != testSecret {
				t.Fatalf("Get() from the successor = %q, %v, %v", value, found, err)
			}
			if earlierNames {
				// What the successor placed beside the earlier socket is gone once it took over.
				for _, path := range []string{socket, socket + ".lock"} {
					if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
						t.Errorf("%s is %v after the handover, want it removed", path, err)
					}
				}
			}
			if err := client.Lock(ctx); err != nil {
				t.Fatalf("Lock() = %v", err)
			}
			if _, err := client.Status(ctx); !errors.Is(err, ErrNotRunning) {
				t.Fatalf("Status() after the lock = %v, want ErrNotRunning", err)
			}
		})
	}
}

// A successor refuses a handover in the earlier format that does not say when it locks, which would
// extend the unlock, and one it cannot read.
func TestSuccessorRefusesAnEarlierHandoverItCannotTrust(t *testing.T) {
	tv := newTestVault(t)
	predecessor, err := endpointOf(t, tv.vault.Dir()).Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = predecessor.Close() })
	noLock, _ := json.Marshal(vault.Snapshot{Identity: tv.snap.Identity, Secrets: tv.snap.Secrets})
	for name, data := range map[string][]byte{
		"without the lock time": noLock,
		"not readable":          []byte(`{"identity":"AGE-SECRET-KEY-1NOTAKEY","locks_at":"2030-01-01T00:00:00Z"}`),
	} {
		t.Run(name, func(t *testing.T) {
			signer := releasetest.NewSigner(t)
			spec, _ := json.Marshal(helperSpec{Mode: "successor", ConfigPath: tv.dir + "/config.yaml",
				TrustedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))})
			result, err := background.Start(withTimeout(t), background.Command{Program: os.Args[0],
				Env: append(os.Environ(), helperEnv+"="+string(spec))}, data, background.Options{Timeout: 20 * time.Second})
			if err == nil {
				_ = result.Process.Kill()
				t.Fatalf("the successor started with report %q, want it refused", result.Report)
			}
			if strings.Contains(err.Error(), testSecret) {
				t.Fatalf("the refusal quotes the handover: %v", err)
			}
		})
	}
	if _, err := os.Lstat(must(t, endpointOf(t, tv.vault.Dir()).Path) + ".next"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused successor left its socket: %v", err)
	}
}

func must(t *testing.T, f func() (string, error)) string {
	t.Helper()
	s, err := f()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
