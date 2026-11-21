package manage

import (
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

// VaultKind is where a vault stands for every surface, and the one place that says so.
type VaultKind string

const (
	// VaultNone is no vault: no vault is configured, or its directory does not exist yet.
	VaultNone VaultKind = "none"
	// VaultUnencrypted is a vault that holds its document without a passphrase.
	VaultUnencrypted VaultKind = "unencrypted"
	// VaultLocked is an encrypted vault that neither this window nor a vault process holds open.
	VaultLocked VaultKind = "locked"
	// VaultProcessOnly is an encrypted vault that only a vault process holds open: agents can work, but this
	// window has no key and needs the passphrase for anything that reads inside the vault.
	VaultProcessOnly VaultKind = "process-only"
	// VaultThisWindow is an encrypted vault this window unlocked itself.
	VaultThisWindow VaultKind = "this-window"
	// VaultUnreadable is a vault whose state cannot be read at all.
	VaultUnreadable VaultKind = "unreadable"
)

// VaultProcess is the last known status of the vault process, as a surface asked it earlier. It is an input
// to the rule, never asked for by it.
type VaultProcess struct {
	// Unlocked reports a running vault process that holds the vault open.
	Unlocked bool
	// EndsAt is the process's hard unlock deadline; zero without a maximum unlock duration.
	EndsAt time.Time
}

// VaultState is the answer of ClassifyVault.
type VaultState struct {
	Kind VaultKind
	// Local is the state of the vault in this process as read, empty when it could not be read or there is
	// no vault handle.
	Local vault.State
	// EndsAt is the hard unlock deadline: of the vault process for VaultProcessOnly, and of the window's own
	// key for VaultThisWindow once WithWindowKey added it. Zero without one.
	EndsAt time.Time
	// Err is the failure reading the local state; set exactly for VaultUnreadable.
	Err error
}

// ClassifyVault places a vault from its local state, the failure reading it, and the last known status of
// the vault process. It is a pure function: it asks no socket and reads no file, so a surface applies it
// while drawing. A process counts only for a vault locked in this process.
func ClassifyVault(local vault.State, readErr error, process VaultProcess) VaultState {
	if readErr != nil {
		return VaultState{Kind: VaultUnreadable, Err: readErr}
	}
	s := VaultState{Local: local}
	switch local {
	case vault.StateUnencrypted:
		s.Kind = VaultUnencrypted
	case vault.StateUnlocked:
		s.Kind = VaultThisWindow
	case vault.StateLocked:
		if process.Unlocked {
			s.Kind, s.EndsAt = VaultProcessOnly, process.EndsAt
		} else {
			s.Kind = VaultLocked
		}
	default:
		s.Kind = VaultNone
	}
	return s
}

// VaultStateOf reads the local state of v, nil meaning no vault configured, and classifies it.
func VaultStateOf(v *vault.Vault, process VaultProcess) VaultState {
	if v == nil {
		return VaultState{Kind: VaultNone}
	}
	local, err := v.State()
	return ClassifyVault(local, err, process)
}

// Unlocked reports whether the vault counts as unlocked for agents, wherever the key is held.
func (s VaultState) Unlocked() bool {
	return s.Kind == VaultProcessOnly || s.Kind == VaultThisWindow
}

// NeedsWindowKey reports an encrypted vault this window cannot read yet: locked, or open only in a vault
// process.
func (s VaultState) NeedsWindowKey() bool {
	return s.Kind == VaultLocked || s.Kind == VaultProcessOnly
}

// Effective is the vault state as agents see it, in the words of the vault package: a process-held vault is
// unlocked. It is empty for an unreadable vault.
func (s VaultState) Effective() vault.State {
	switch s.Kind {
	case VaultNone:
		return vault.StateAbsent
	case VaultUnencrypted:
		return vault.StateUnencrypted
	case VaultLocked:
		return vault.StateLocked
	case VaultProcessOnly, VaultThisWindow:
		return vault.StateUnlocked
	}
	return ""
}
