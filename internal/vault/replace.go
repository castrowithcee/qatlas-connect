package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-kit/filelock"
)

// writeLockFile guards every write of the vault's document against another process writing it at the same
// time. It starts with a dot, so a backup of the vault's files leaves it out like a temporary file, and it is
// never removed while the vault exists, which keeps the lock stable while the files beside it are replaced by
// rename.
const writeLockFile = ".secrets.lock"

// ErrEmptyReplacement reports a replacement without an old or a new value: a replacement never creates a
// role and never empties one.
var ErrEmptyReplacement = errors.New("vault: a replacement needs the old and the new value")

// ReplaceRole replaces the value of one role of the entry name with value, but only while the role still
// holds old. It reads the vault as it is on disk, under the lock that every write of this vault takes, so of
// two replacements based on the same old value exactly one wins. It reports the value the role holds
// afterwards: value when it was replaced, otherwise whatever another writer stored, "" when the role holds
// none. It never creates or removes an entry or a role.
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
	_, err = v.update(v.writeIdentity(state), updateOpts{keep: true}, func(doc *document) (bool, error) {
		stored, found := roleOf(doc, name, role)
		if !found || stored != old {
			current = stored
			return false, nil
		}
		doc.setRole(name, "", role, value, time.Now().UTC())
		current, replaced = value, true
		return true, nil
	})
	if err != nil {
		return "", false, err
	}
	return current, replaced, nil
}

// readSecrets reads and decrypts secrets.age as it is on disk; a vault without one is an empty document.
func (v *Vault) readSecrets(key age.Identity) (*document, error) {
	data, _, err := readExisting(v.secretsPath())
	if err != nil {
		return nil, err
	}
	return decodeSecrets(v.secretsPath(), data, key)
}

// lockWrites takes the vault's write lock and returns the function that releases it. Every write of the
// document, the key or the recipient takes it, and reads what it changes only once it holds it.
//
// The lock is a lock on an open file, and a second open file of the same process waits for the first like any
// other process does, so it must never be taken while it is held: a write takes it once, at its outermost
// call, and everything it calls with the lock held is a helper that takes none.
func (v *Vault) lockWrites() (func(), error) {
	if err := mkdirPrivate(v.dir); err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", v.dir, err)
	}
	path := filepath.Join(v.dir, writeLockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, fmt.Errorf("cannot open the lock file %s: %w", path, err)
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}
