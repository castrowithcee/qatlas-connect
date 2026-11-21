package manage

import (
	"errors"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
)

// WindowKey is a window's own hold on the key of an encrypted vault it unlocked itself, as the window keeps
// it between two asks of the vault process. The rules below decide from it; they ask no socket.
type WindowKey struct {
	// Held reports that the window holds the vault key itself.
	Held bool
	// Since is when the window unlocked the vault itself; zero while it does not hold the key.
	Since time.Time
	// SeenPID is the vault process the window has seen holding the vault open since it unlocked it; 0 for
	// none yet.
	SeenPID int
	// Busy reports the window's own unlock, hand-off to a vault process or other vault work still in
	// flight.
	Busy bool
}

// LockedElsewhere reports whether one answer of the vault process counts as a lock outside the window: the
// window holds the key, is not busy, has seen a vault process since its own unlock, and now there is none
// or another one. Another process counts even after an update handed the vault to a new program: the window
// then asks once more for the passphrase. A replaced process, a timeout, a refused peer or any other failure
// of the ask leaves the window's key alone.
func LockedElsewhere(key WindowKey, status vaultproc.Status, askErr error) bool {
	if !key.Held || key.Busy || key.SeenPID == 0 {
		return false
	}
	if askErr != nil {
		return errors.Is(askErr, vaultproc.ErrNotRunning) && !errors.Is(askErr, vaultproc.ErrReplaced)
	}
	return status.PID != key.SeenPID
}

// Seen is key with the vault process of one answer remembered: the first one the window sees holding the
// vault open since its own unlock. Any other answer leaves key as it is.
func (k WindowKey) Seen(status vaultproc.Status, askErr error) WindowKey {
	if k.Held && !k.Busy && k.SeenPID == 0 && askErr == nil && status.PID > 0 {
		k.SeenPID = status.PID
	}
	return k
}

// EndsAt is when the window's own key ends by the maximum unlock duration: only while no vault process was
// seen since the window's unlock, which otherwise ends the key by its own lock. Zero means no deadline,
// also for a maxUnlock of zero.
func (k WindowKey) EndsAt(maxUnlock time.Duration) time.Time {
	if !k.Held || k.SeenPID != 0 || maxUnlock <= 0 || k.Since.IsZero() {
		return time.Time{}
	}
	return k.Since.Add(maxUnlock)
}

// Expired reports whether the window's own key has reached its deadline at now and should be forgotten. A
// busy window keeps it until its work is done.
func (k WindowKey) Expired(maxUnlock time.Duration, now time.Time) bool {
	ends := k.EndsAt(maxUnlock)
	return !k.Busy && !ends.IsZero() && !now.Before(ends)
}

// WithWindowKey adds the deadline of the window's own key to a vault the window unlocked itself, so a
// surface shows it as it shows the deadline of a vault process.
func (s VaultState) WithWindowKey(key WindowKey, maxUnlock time.Duration) VaultState {
	if s.Kind == VaultThisWindow {
		s.EndsAt = key.EndsAt(maxUnlock)
	}
	return s
}

// MaxUnlock is the maximum unlock duration in force for v (see vault.EffectiveSettings): zero, meaning no
// limit, for a vault that is locked, unencrypted or absent, whose key a window does not hold anyway.
func MaxUnlock(v *vault.Vault) time.Duration {
	settings, _ := vault.EffectiveSettings(v)
	return settings.MaxUnlock
}
