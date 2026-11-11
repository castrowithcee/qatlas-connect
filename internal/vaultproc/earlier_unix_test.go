//go:build (linux || darwin) && !legacy_vault_names

package vaultproc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// earlierFixture returns a vault directory and the socket the earlier release keeps for it, in a runtime
// directory of the test's own.
func earlierFixture(t *testing.T) (vaultDir, legacy string) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))
	vaultDir = filepath.Join(t.TempDir(), "vault")
	legacy, err := socketPath(vaultDir, legacyRuntimeDirName)
	if err != nil {
		t.Fatal(err)
	}
	// The channel FindEarlier looks at is the socket the earlier release keeps, by its own names.
	e, ok := earlierEndpoint(vaultDir)
	if !ok {
		t.Fatal("earlierEndpoint() found no channel of the earlier release")
	}
	if path, err := e.Path(); err != nil || path != legacy {
		t.Fatalf("the earlier channel is %q, %v, want %q", path, err, legacy)
	}
	return vaultDir, legacy
}

// startEarlier starts a helper process that serves path as a vault process of the earlier release does.
func startEarlier(t *testing.T, path string) (*exec.Cmd, func() string) {
	t.Helper()
	cmd, out := helper(t, os.Args[0], "serve-earlier", path)
	if got := line(t, out); got != "listening" {
		t.Fatalf("the helper process said %q", got)
	}
	return cmd, func() string { return line(t, out) }
}

// A vault process of the earlier release is found by the kernel's credentials of its socket and ended with
// SIGTERM, on which it locks itself; afterwards nothing is found, and this release's socket was never
// touched.
func TestEndEarlierEndsTheEarlierProcess(t *testing.T) {
	vaultDir, legacy := earlierFixture(t)
	cmd, said := startEarlier(t, legacy)
	if pid, err := FindEarlier(vaultDir); err != nil || pid != cmd.Process.Pid {
		t.Fatalf("FindEarlier() = %d, %v, want the helper's pid %d", pid, err, cmd.Process.Pid)
	}
	pid, err := EndEarlier(context.Background(), vaultDir)
	if err != nil || pid != cmd.Process.Pid {
		t.Fatalf("EndEarlier() = %d, %v, want the helper's pid %d", pid, err, cmd.Process.Pid)
	}
	if got := said(); got != "locked" {
		t.Fatalf("the helper process said %q after EndEarlier(), want it locked", got)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the helper process ended with %v", err)
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
	assertAbsent(t, current, legacy)
}

// Only a socket of this user in a private directory, with its lock held and a process listening, is an
// earlier vault process's. Nothing else is found or ended, and nothing else is reported either.
func TestFindEarlierIgnoresWhatIsNoEarlierProcess(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, vaultDir, legacy string){
		"nothing": func(*testing.T, string, string) {},
		"socket without its lock": func(t *testing.T, _, legacy string) {
			prepareSocketDir(t, legacy)
			l, err := rawListen(legacy)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = l.Close() })
			if err := os.Chmod(legacy, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"orphaned socket and lock": func(t *testing.T, _, legacy string) {
			prepareSocketDir(t, legacy)
			if _, err := bindOnly(legacy); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacy+".lock", nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"directory open to others": func(t *testing.T, _, legacy string) {
			startEarlier(t, legacy)
			if err := os.Chmod(filepath.Dir(legacy), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"this release's socket": func(t *testing.T, vaultDir, _ string) {
			current, err := SocketPath(vaultDir)
			if err != nil {
				t.Fatal(err)
			}
			startEarlier(t, current)
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

// A socket at the earlier place that others may open fails localipc's checks of a channel: it is reported
// and left alone, whether or not a lock beside it is held.
func TestEarlierSocketNotPrivateIsNotEnded(t *testing.T) {
	vaultDir, legacy := earlierFixture(t)
	prepareSocketDir(t, legacy)
	l, err := rawListen(legacy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := os.Chmod(legacy, 0o660); err != nil {
		t.Fatal(err)
	}
	if pid, err := FindEarlier(vaultDir); pid != 0 || !errors.Is(err, ErrEarlierUnknown) {
		t.Fatalf("FindEarlier() = %d, %v, want ErrEarlierUnknown", pid, err)
	}
	if pid, err := EndEarlier(context.Background(), vaultDir); pid != 0 || !errors.Is(err, ErrEarlierUnknown) {
		t.Fatalf("EndEarlier() = %d, %v, want ErrEarlierUnknown", pid, err)
	}
}

// A served earlier socket whose process cannot be identified as another process of this user is reported
// and left alone. A process of another user and unreadable credentials are refused the same way by
// localipc's Holder, whose own tests cover them.
func TestEarlierProcessThatCannotBeIdentifiedIsNotEnded(t *testing.T) {
	vaultDir, legacy := earlierFixture(t)
	listenFor(t, legacy)
	if pid, err := FindEarlier(vaultDir); pid != 0 || !errors.Is(err, ErrEarlierUnknown) {
		t.Fatalf("FindEarlier() = %d, %v, want ErrEarlierUnknown", pid, err)
	}
	if pid, err := EndEarlier(context.Background(), vaultDir); pid != 0 || !errors.Is(err, ErrEarlierUnknown) {
		t.Fatalf("EndEarlier() = %d, %v, want ErrEarlierUnknown", pid, err)
	}
	if !serves(legacy) {
		t.Fatal("the socket is no longer served")
	}
}
