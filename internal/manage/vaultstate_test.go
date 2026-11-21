package manage

import (
	"errors"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

func TestClassifyVaultEveryCombination(t *testing.T) {
	ends := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	readErr := errors.New("permission denied")
	cases := []struct {
		name      string
		local     vault.State
		err       error
		process   VaultProcess
		want      VaultKind
		wantEnds  time.Time
		effective vault.State
	}{
		{"absent", vault.StateAbsent, nil, VaultProcess{}, VaultNone, time.Time{}, vault.StateAbsent},
		{"absent with a process", vault.StateAbsent, nil, VaultProcess{Unlocked: true, EndsAt: ends}, VaultNone, time.Time{}, vault.StateAbsent},
		{"unencrypted", vault.StateUnencrypted, nil, VaultProcess{}, VaultUnencrypted, time.Time{}, vault.StateUnencrypted},
		{"unencrypted with a process", vault.StateUnencrypted, nil, VaultProcess{Unlocked: true, EndsAt: ends}, VaultUnencrypted, time.Time{}, vault.StateUnencrypted},
		{"locked", vault.StateLocked, nil, VaultProcess{}, VaultLocked, time.Time{}, vault.StateLocked},
		{"locked, stale deadline without a process", vault.StateLocked, nil, VaultProcess{EndsAt: ends}, VaultLocked, time.Time{}, vault.StateLocked},
		{"process only", vault.StateLocked, nil, VaultProcess{Unlocked: true}, VaultProcessOnly, time.Time{}, vault.StateUnlocked},
		{"process only with a deadline", vault.StateLocked, nil, VaultProcess{Unlocked: true, EndsAt: ends}, VaultProcessOnly, ends, vault.StateUnlocked},
		{"this window", vault.StateUnlocked, nil, VaultProcess{}, VaultThisWindow, time.Time{}, vault.StateUnlocked},
		{"this window and a process", vault.StateUnlocked, nil, VaultProcess{Unlocked: true, EndsAt: ends}, VaultThisWindow, time.Time{}, vault.StateUnlocked},
		{"unreadable", "", readErr, VaultProcess{}, VaultUnreadable, time.Time{}, ""},
		{"unreadable with a process", "", readErr, VaultProcess{Unlocked: true, EndsAt: ends}, VaultUnreadable, time.Time{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyVault(c.local, c.err, c.process)
			if got.Kind != c.want || !got.EndsAt.Equal(c.wantEnds) {
				t.Errorf("ClassifyVault = %s ending %v, want %s ending %v", got.Kind, got.EndsAt, c.want, c.wantEnds)
			}
			if (got.Err != nil) != (c.want == VaultUnreadable) {
				t.Errorf("Err = %v for %s", got.Err, c.want)
			}
			if got.Effective() != c.effective {
				t.Errorf("Effective = %q, want %q", got.Effective(), c.effective)
			}
			if wantUnlocked := c.want == VaultProcessOnly || c.want == VaultThisWindow; got.Unlocked() != wantUnlocked {
				t.Errorf("Unlocked = %v, want %v", got.Unlocked(), wantUnlocked)
			}
			if wantKey := c.want == VaultLocked || c.want == VaultProcessOnly; got.NeedsWindowKey() != wantKey {
				t.Errorf("NeedsWindowKey = %v, want %v", got.NeedsWindowKey(), wantKey)
			}
		})
	}
}

func TestVaultStateOfNoHandle(t *testing.T) {
	if got := VaultStateOf(nil, VaultProcess{Unlocked: true}); got.Kind != VaultNone {
		t.Errorf("VaultStateOf(nil) = %s, want %s", got.Kind, VaultNone)
	}
}
