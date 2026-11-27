package vault

import (
	"errors"
	"time"

	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// ErrEmptyReplacement reports a replacement without an old or a new value: a replacement never creates a
// role and never empties one.
var ErrEmptyReplacement = errors.New("vault: a replacement needs the old and the new value")

// ReplaceRole replaces the value of one role of the entry name with value, but only while the role still
// holds old. It reads the vault as it is on disk, under the lock that every write of this vault takes, so of
// two replacements based on the same old value exactly one wins. It reports the value the role holds
// afterwards: value when it was replaced, otherwise whatever another writer stored, "" when the role holds
// none. It never creates or removes an entry or a role. An open Checkpoint never takes a replacement back,
// because the old value may no longer be valid.
//
// An unencrypted vault is replaced directly. An encrypted one needs its key: it must be unlocked in this
// process or opened with OpenWithKey. Pending entries are merged first, as an unlock does, so a value queued
// while the vault was locked counts as stored and is never merged over the replacement later.
func (v *Vault) ReplaceRole(name, role, old, value string) (current string, replaced bool, err error) {
	if old == "" || value == "" {
		return "", false, ErrEmptyReplacement
	}
	state, err := v.State()
	if err != nil {
		return "", false, err
	}
	switch state {
	case StateAbsent:
		return "", false, nil
	case StateLocked:
		return "", false, ErrNotUnlocked
	}
	_, _, err = v.update(func(doc *document) (bool, error) {
		stored, found := roleOf(doc, name, role)
		if !found || stored != old {
			current = stored
			return false, nil
		}
		doc.setRole(name, "", role, value, time.Now().UTC())
		current, replaced = value, true
		return true, nil
	}, kitvault.ExemptFromCheckpoints())
	if err != nil {
		return "", false, err
	}
	return current, replaced, nil
}
