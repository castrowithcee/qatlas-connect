//go:build (linux || darwin) && !legacy_vault_names

package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
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
	opts := ServeOptions{ConfigPath: spec.ConfigPath, Successor: true, TrustedKeys: []ssh.PublicKey{key}}
	if spec.ToConfig != "" {
		opts.SuccessorConfig = func(string, *age.X25519Identity) (string, error) { return spec.ToConfig, nil }
	}
	if err := Serve(context.Background(), opts); err != nil {
		return 1
	}
	return 0
}

// earlierHandover is what a vault process of a release before the kit's channel, up to protocol version 10,
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

// A successor that serves another vault directory than the one handed over, as after a migration, takes the
// vault over only from a vault process of the earlier release that serves the directory handed over, and only
// while none of this release serves its own. Otherwise it does not start and places nothing anywhere, beside
// a process that serves the new directory least of all.
func TestSuccessorForAnotherVaultDirectoryNeedsAnEarlierProcess(t *testing.T) {
	for name, tc := range map[string]struct {
		earlier, current, starts bool
	}{
		"nothing serves":                        {},
		"this release serves the new directory": {earlier: true, current: true},
		"an earlier process serves":             {earlier: true, starts: true},
	} {
		t.Run(name, func(t *testing.T) {
			tv := newTestVault(t)
			to := filepath.Join(t.TempDir(), "migrated")
			if err := os.MkdirAll(to, 0o700); err != nil {
				t.Fatal(err)
			}
			toConfig := filepath.Join(to, "config.yaml")
			if err := os.WriteFile(toConfig, []byte("version: 4\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			toVault := vault.New(to).Dir()
			earlier, ok := earlierEndpoint(tv.vault.Dir())
			if !ok {
				t.Fatal("no channel of the earlier release")
			}
			earlierSocket := must(t, earlier.Path)
			currentSocket := must(t, endpointOf(t, toVault).Path)
			if tc.earlier {
				l, err := earlier.Listen()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = l.Close() })
			}
			if tc.current {
				l, err := endpointOf(t, toVault).Listen()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = l.Close() })
			}

			signer := releasetest.NewSigner(t)
			spec, _ := json.Marshal(helperSpec{Mode: "successor", ConfigPath: tv.dir + "/config.yaml",
				ToConfig: toConfig, TrustedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))})
			result, err := background.Start(withTimeout(t), background.Command{Program: os.Args[0],
				Env: append(os.Environ(), helperEnv+"="+string(spec))},
				earlierHandover(t, tv, time.Now().Add(time.Hour), time.Now()), background.Options{Timeout: 20 * time.Second})
			if err == nil {
				t.Cleanup(func() {
					_ = result.Process.Kill()
					<-result.Process.Done()
				})
			}
			if tc.starts {
				if err != nil {
					t.Fatalf("the successor did not start: %v", err)
				}
				return
			}
			// The kit reports a refusal of Configure with a fixed reason.
			var report *background.ReportError
			if !errors.As(err, &report) || report.Reason != "the vault process settings cannot be read" {
				t.Fatalf("the successor started = %v, want it refused as it configures", err)
			}
			for _, path := range []string{currentSocket + ".next", currentSocket + ".next.lock",
				earlierSocket + ".next", earlierSocket + ".next.lock"} {
				if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("a refused successor left %s: %v", path, err)
				}
			}
			if tc.current {
				if _, err := os.Lstat(currentSocket); err != nil {
					t.Errorf("the socket of the process that serves the new directory is gone: %v", err)
				}
			} else if _, err := os.Lstat(currentSocket); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a refused successor left its socket: %v", err)
			}
		})
	}
}

// The successor's check of another vault directory refuses with the reason Connect always gave.
func TestSuccessorEarlierRefusesAnotherDirectoryWithoutAnEarlierProcess(t *testing.T) {
	tv := newTestVault(t)
	if e, err := successorEarlier(tv.vault.Dir(), tv.vault.Dir()); err != nil || e == nil {
		t.Fatalf("successorEarlier() for the same directory = %v, %v, want the earlier channel", e, err)
	}
	other := filepath.Join(t.TempDir(), "vault")
	if _, err := successorEarlier(tv.vault.Dir(), other); err == nil ||
		err.Error() != "no vault process of the earlier release serves the vault handed over" {
		t.Fatalf("successorEarlier() for another directory = %v, want the refusal", err)
	}
}
