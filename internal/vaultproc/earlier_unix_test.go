//go:build (linux || darwin) && !legacy_vault_names

package vaultproc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

// legacySocket is the socket the release before qatlas-connect keeps for vaultDir below XDG_RUNTIME_DIR, by
// that release's own formula.
func legacySocket(t *testing.T, vaultDir string) string {
	t.Helper()
	abs, err := filepath.Abs(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "qatlas", "vault-"+hex.EncodeToString(sum[:8])+".sock")
}

// earlierFixture returns a vault directory and the socket the earlier release keeps for it, in a runtime
// directory of the test's own.
func earlierFixture(t *testing.T) (vaultDir, legacy string) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))
	vaultDir = filepath.Join(t.TempDir(), "vault")
	legacy = legacySocket(t, vaultDir)
	e, ok := earlierEndpoint(vaultDir)
	if !ok {
		t.Fatal("earlierEndpoint() found no channel of the earlier release")
	}
	if path, err := e.Path(); err != nil || path != legacy {
		t.Fatalf("the earlier channel is %q, %v, want %q", path, err, legacy)
	}
	return vaultDir, legacy
}

// startEarlier starts a helper process that serves the earlier release's channel of vaultDir as a vault
// process of that release does.
func startEarlier(t *testing.T, vaultDir string) *helperProcess {
	t.Helper()
	return startHelper(t, helperSpec{Mode: "serve", VaultDir: vaultDir, RuntimeDir: legacyRuntimeDirName,
		Line: `{"v":10,"error":"version"}` + "\n"}, "listening")
}

// A vault process of the earlier release is found as another process of this user behind a private channel
// with its lock held, and ended with SIGTERM, on which it locks itself; afterwards nothing is found, and this
// release's socket was never touched.
func TestEndEarlierEndsTheEarlierProcess(t *testing.T) {
	vaultDir, legacy := earlierFixture(t)
	h := startEarlier(t, vaultDir)
	if pid, err := FindEarlier(vaultDir); err != nil || pid != h.pid() {
		t.Fatalf("FindEarlier() = %d, %v, want the helper's pid %d", pid, err, h.pid())
	}
	if !EarlierServes(vaultDir) {
		t.Error("EarlierServes() = false while the earlier process serves the vault")
	}
	pid, err := EndEarlier(context.Background(), vaultDir)
	if err != nil || pid != h.pid() {
		t.Fatalf("EndEarlier() = %d, %v, want the helper's pid %d", pid, err, h.pid())
	}
	if got := h.line(t); got != "locked" {
		t.Fatalf("the helper process said %q after EndEarlier(), want it locked", got)
	}
	if !h.ended(5 * time.Second) {
		t.Fatal("the helper process still runs")
	}
	if pid, err := FindEarlier(vaultDir); pid != 0 || err != nil {
		t.Fatalf("FindEarlier() after the end = %d, %v, want none", pid, err)
	}
	if pid, err := EndEarlier(context.Background(), vaultDir); pid != 0 || err != nil {
		t.Fatalf("EndEarlier() after the end = %d, %v, want none", pid, err)
	}
	current, err := SocketPath(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{current, legacy} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is %v, want it gone", path, err)
		}
	}
}

// Only a private socket of this user in a private directory, with its lock held and a process listening,
// is an earlier vault process's. Nothing else is found or ended, and nothing else is reported either: a
// socket without its lock, or in a directory open to others, is no channel localipc serves.
func TestFindEarlierIgnoresWhatIsNoEarlierProcess(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, vaultDir, legacy string){
		"nothing": func(*testing.T, string, string) {},
		"orphaned socket and lock": func(t *testing.T, _, legacy string) {
			l := listenRaw(t, legacy)
			if ul, ok := l.(interface{ SetUnlinkOnClose(bool) }); ok {
				ul.SetUnlinkOnClose(false)
			}
			_ = l.Close()
			if err := os.Chmod(legacy, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacy+".lock", nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"socket without its lock": func(t *testing.T, _, legacy string) {
			listenRaw(t, legacy)
			if err := os.Chmod(legacy, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"directory open to others": func(t *testing.T, vaultDir, legacy string) {
			startEarlier(t, vaultDir)
			if err := os.Chmod(filepath.Dir(legacy), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"this release's channel": func(t *testing.T, vaultDir, _ string) {
			startHelper(t, helperSpec{Mode: "serve", VaultDir: vaultDir, Line: "\n"}, "listening")
		},
	} {
		t.Run(name, func(t *testing.T) {
			vaultDir, legacy := earlierFixture(t)
			setup(t, vaultDir, legacy)
			if pid, err := FindEarlier(vaultDir); pid != 0 || err != nil {
				t.Fatalf("FindEarlier() = %d, %v, want none", pid, err)
			}
			if pid, err := EndEarlier(context.Background(), vaultDir); pid != 0 || err != nil {
				t.Fatalf("EndEarlier() = %d, %v, want none", pid, err)
			}
		})
	}
}

// What serves the earlier channel but fails a check of localipc is reported as ErrEarlierUnknown and never
// ended: a socket others may open, and this very process. A process of another user and credentials that cannot be read are refused the same way by
// localipc's Holder, whose own tests cover them; earlierPID turns every such refusal into ErrEarlierUnknown.
func TestEarlierThatFailsACheckIsNotEnded(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, vaultDir, legacy string) func() bool{
		"socket open to others": func(t *testing.T, vaultDir, legacy string) func() bool {
			h := startEarlier(t, vaultDir)
			if err := os.Chmod(legacy, 0o660); err != nil {
				t.Fatal(err)
			}
			return func() bool { return !h.ended(0) }
		},
		"this process": func(t *testing.T, vaultDir, _ string) func() bool {
			e, _ := earlierEndpoint(vaultDir)
			l, err := e.Listen()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = l.Close() })
			go answerEvery(l, "\n")
			return func() bool { return true }
		},
	} {
		t.Run(name, func(t *testing.T) {
			vaultDir, legacy := earlierFixture(t)
			alive := setup(t, vaultDir, legacy)
			if pid, err := FindEarlier(vaultDir); pid != 0 || !errors.Is(err, ErrEarlierUnknown) {
				t.Fatalf("FindEarlier() = %d, %v, want ErrEarlierUnknown", pid, err)
			}
			if pid, err := EndEarlier(context.Background(), vaultDir); pid != 0 || !errors.Is(err, ErrEarlierUnknown) {
				t.Fatalf("EndEarlier() = %d, %v, want ErrEarlierUnknown", pid, err)
			}
			if EarlierServes(vaultDir) {
				t.Error("EarlierServes() = true for a channel that fails a check")
			}
			time.Sleep(100 * time.Millisecond)
			if !alive() {
				t.Fatal("what serves the earlier channel was ended or removed")
			}
		})
	}
}

// Where no runtime directory exists, as on macOS without XDG_RUNTIME_DIR, the earlier release and this one
// both keep the socket in run beside the vault, so there is no earlier channel apart from this release's.
// The endpoint names that place as both releases did: <parent>/run/vault-<hash>.sock.
func TestFallbackSocketBesideTheVault(t *testing.T) {
	vaultDir := filepath.Join(shortTempDir(t), "vault")
	abs, _ := filepath.Abs(vaultDir)
	sum := sha256.Sum256([]byte(abs))
	want := filepath.Join(filepath.Dir(abs), "run", "vault-"+hex.EncodeToString(sum[:8])+".sock")
	current := endpointOf(t, vaultDir)
	earlier, err := endpointNamed(vaultDir, legacyRuntimeDirName, legacyPipeName, "earlier")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []localipc.Endpoint{current, earlier} {
		if got := filepath.Join(e.FallbackDir, e.FallbackDirName, e.SocketBase+".sock"); got != want {
			t.Fatalf("the fallback socket of %+v is %s, want %s", e, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join("/run/user", strconv.Itoa(os.Getuid()))); err == nil && runtime.GOOS == "linux" {
		t.Skip("this system has a runtime directory of the user, which Linux takes without XDG_RUNTIME_DIR")
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if path, err := SocketPath(vaultDir); err != nil || path != want {
		t.Fatalf("SocketPath() without a runtime directory = %q, %v, want %q", path, err, want)
	}
	if e, ok := earlierEndpoint(vaultDir); ok {
		t.Fatalf("earlierEndpoint() without a runtime directory = %+v, want none apart from this release's", e)
	}
}
