package manage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
)

// Every combination of the window's own key, the process last seen, its own work in flight and one answer of
// the vault process: only no process or another one counts as a lock, and only for a window that holds the
// key, is idle and has seen a process since its unlock.
func TestLockedElsewhereEveryCombination(t *testing.T) {
	const seen, other = 4100, 4200
	answers := []struct {
		name  string
		pid   int
		err   error
		locks bool
	}{
		{"no process", 0, vaultproc.ErrNotRunning, true},
		{"another process", other, nil, true},
		{"the same process", seen, nil, false},
		{"a replaced process", 0, vaultproc.ErrReplaced, false},
		{"a caller timeout", 0, context.DeadlineExceeded, false},
		{"no answer in time", 0, fmt.Errorf("the vault process did not answer in time: %w", os.ErrDeadlineExceeded), false},
		{"a refused peer", 0, &vaultproc.PeerError{PID: other, Err: vaultproc.ErrRefused}, false},
	}
	for _, held := range []bool{false, true} {
		for _, seenPID := range []int{0, seen} {
			for _, busy := range []bool{false, true} {
				for _, a := range answers {
					name := fmt.Sprintf("held=%v/seen=%d/busy=%v/%s", held, seenPID, busy, a.name)
					t.Run(name, func(t *testing.T) {
						key := WindowKey{Held: held, SeenPID: seenPID, Busy: busy}
						want := held && !busy && seenPID != 0 && a.locks
						if got := LockedElsewhere(key, vaultproc.Status{PID: a.pid}, a.err); got != want {
							t.Errorf("LockedElsewhere = %v, want %v", got, want)
						}
					})
				}
			}
		}
	}
}

// The window remembers the first process it sees holding the vault since its own unlock, and only then.
func TestWindowKeySeenRemembersTheFirstProcess(t *testing.T) {
	running := vaultproc.Status{PID: 4100}
	cases := []struct {
		name   string
		key    WindowKey
		status vaultproc.Status
		err    error
		want   int
	}{
		{"first process", WindowKey{Held: true}, running, nil, 4100},
		{"already seen one", WindowKey{Held: true, SeenPID: 4000}, running, nil, 4000},
		{"no key", WindowKey{}, running, nil, 0},
		{"busy", WindowKey{Held: true, Busy: true}, running, nil, 0},
		{"no process", WindowKey{Held: true}, vaultproc.Status{}, vaultproc.ErrNotRunning, 0},
		{"failed ask", WindowKey{Held: true}, running, context.DeadlineExceeded, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.key.Seen(c.status, c.err).SeenPID; got != c.want {
				t.Errorf("Seen().SeenPID = %d, want %d", got, c.want)
			}
		})
	}
}

// Without a vault process seen since the unlock, the window's key ends after the maximum unlock duration
// counted from its own unlock; zero means no limit, and a process seen takes over.
func TestWindowKeyEndsAfterTheMaximumUnlockDuration(t *testing.T) {
	since := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	key := WindowKey{Held: true, Since: since}
	if got := key.EndsAt(time.Hour); !got.Equal(since.Add(time.Hour)) {
		t.Fatalf("EndsAt = %v, want an hour after the unlock", got)
	}
	cases := []struct {
		name string
		key  WindowKey
		max  time.Duration
		now  time.Time
		want bool
	}{
		{"before the deadline", key, time.Hour, since.Add(59 * time.Minute), false},
		{"at the deadline", key, time.Hour, since.Add(time.Hour), true},
		{"after the deadline", key, time.Hour, since.Add(2 * time.Hour), true},
		{"no limit", key, 0, since.Add(1000 * time.Hour), false},
		{"busy", WindowKey{Held: true, Since: since, Busy: true}, time.Hour, since.Add(2 * time.Hour), false},
		{"a process seen", WindowKey{Held: true, Since: since, SeenPID: 4100}, time.Hour, since.Add(2 * time.Hour), false},
		{"no key", WindowKey{Since: since}, time.Hour, since.Add(2 * time.Hour), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.key.Expired(c.max, c.now); got != c.want {
				t.Errorf("Expired = %v, want %v", got, c.want)
			}
		})
	}
}

// Only a vault the window unlocked itself carries the deadline of its own key; a process-only vault keeps the
// deadline of the process.
func TestWithWindowKeyAddsTheOwnDeadline(t *testing.T) {
	since := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	process := since.Add(5 * time.Hour)
	key := WindowKey{Held: true, Since: since}
	own := ClassifyVault(vault.StateUnlocked, nil, VaultProcess{}).WithWindowKey(key, time.Hour)
	if !own.EndsAt.Equal(since.Add(time.Hour)) {
		t.Errorf("this window: EndsAt = %v, want an hour after the unlock", own.EndsAt)
	}
	unlimited := ClassifyVault(vault.StateUnlocked, nil, VaultProcess{}).WithWindowKey(key, 0)
	if !unlimited.EndsAt.IsZero() {
		t.Errorf("this window without a limit: EndsAt = %v, want none", unlimited.EndsAt)
	}
	elsewhere := ClassifyVault(vault.StateLocked, nil, VaultProcess{Unlocked: true, EndsAt: process}).
		WithWindowKey(WindowKey{}, time.Hour)
	if !elsewhere.EndsAt.Equal(process) {
		t.Errorf("process only: EndsAt = %v, want the process deadline", elsewhere.EndsAt)
	}
}
