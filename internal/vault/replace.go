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

// writeLockFile guards a replacement against another process replacing the same vault at the same time. It
// starts with a dot, so a backup of the vault's files leaves it out like a temporary file, and it is never
// removed, which keeps the lock stable while the files beside it are replaced by rename.
const writeLockFile = ".secrets.lock"

// ErrEmptyReplacement reports a replacement without an old or a new value: a replacement never creates a
// role and never empties one.
var ErrEmptyReplacement = errors.New("vault: a replacement needs the old and the new value")

// ReplaceRole replaces the value of one role of the entry name with value, but only while the role still
// holds old. It reads the vault as it is on disk, under a lock that every replacement of this vault takes,
// so of two replacements based on the same old value exactly one wins. It reports the value the role holds
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
	unlock, err := v.lockWrites()
	if err != nil {
		return "", false, err
	}
	defer unlock()

	now := time.Now().UTC()
	if state == StateUnencrypted {
		doc, err := loadPlainDocument(v.plainPath())
		if err != nil {
			return "", false, err
		}
		current, found := roleOf(doc, name, role)
		if !found || current != old {
			return current, false, nil
		}
		doc.setRole(name, "", role, value, now)
		if err := v.writePlain(doc); err != nil {
			return "", false, err
		}
		return value, true, nil
	}

	doc, err := v.readSecrets(v.identity.key)
	if err != nil {
		return "", false, err
	}
	pending, err := v.pendingFiles()
	if err != nil {
		return "", false, err
	}
	merged, err := v.mergePending(v.identity, doc)
	if err != nil {
		return "", false, err
	}
	current, found := roleOf(doc, name, role)
	if !found || current != old {
		if found && v.doc != nil {
			// Later reads of this process see what another writer stored; nothing is written.
			v.doc.setRole(name, "", role, current, now)
		}
		return current, false, nil
	}
	doc.setRole(name, "", role, value, now)
	if err := v.writeSecrets(v.identity, doc); err != nil {
		return "", false, err
	}
	if merged > 0 {
		v.removePending(pending)
	}
	v.doc = doc
	return value, true, nil
}

// readSecrets reads and decrypts secrets.age as it is on disk; a vault without one is an empty document.
func (v *Vault) readSecrets(key age.Identity) (*document, error) {
	data, err := readFile(v.secretsPath())
	if isNotExist(err) {
		return newDocument(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", v.secretsPath(), err)
	}
	plain, err := decryptFrom(data, key)
	if err != nil {
		return nil, fmt.Errorf("cannot decrypt %s: %w", v.secretsPath(), err)
	}
	defer clear(plain)
	return decodeDocument(v.secretsPath(), plain)
}

// lockWrites takes the vault's write lock and returns the function that releases it.
func (v *Vault) lockWrites() (func(), error) {
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
