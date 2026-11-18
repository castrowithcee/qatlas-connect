package vault

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// The prompts a PassphraseFunc is asked with. An implementation may show its own text instead; these are
// the default an interactive terminal prompt uses.
const (
	unlockPrompt = "vault passphrase: "
	createPrompt = "passphrase to encrypt the vault, or leave empty to store it unencrypted: "
)

// Get returns the secret of one credential role. It reports the state the vault was in while answering,
// which becomes StateUnlocked once a locked vault was opened to answer it. ask is asked for the passphrase
// only when the vault is encrypted and locked; a nil ask, or one that returns ErrNoTerminal, leaves the
// vault locked and reports that error. found is false when the vault holds nothing for the pair, which is
// not itself an error.
func (v *Vault) Get(name, role string, ask PassphraseFunc) (value string, found bool, state State, err error) {
	state, err = v.State()
	if err != nil {
		return "", false, state, err
	}
	switch state {
	case StateAbsent:
		return "", false, state, nil
	case StateUnencrypted:
		doc, err := loadPlainDocument(v.plainPath())
		if err != nil {
			return "", false, state, err
		}
		value, found = roleOf(doc, name, role)
		return value, found, state, nil
	case StateLocked:
		if err := v.ensureUnlocked(ask); err != nil {
			return "", false, StateLocked, err
		}
		state = StateUnlocked
		fallthrough
	case StateUnlocked:
		value, found = roleOf(v.doc, name, role)
		return value, found, state, nil
	}
	return "", false, state, fmt.Errorf("vault: unreachable state %q", state)
}

func roleOf(doc *document, name, role string) (string, bool) {
	entry, ok := doc.byName(name)
	if !ok {
		return "", false
	}
	value, ok := entry.Roles[role]
	return value, ok
}

// Set stores the secret of one credential role, creating the vault when this is its first secret. offer is
// asked for a passphrase only then; an empty passphrase, or a nil offer, creates the vault unencrypted.
// While the vault is encrypted and locked, Set never needs the passphrase: it writes a pending entry,
// encrypted to the vault's public recipient, which is merged in on the next unlock.
func (v *Vault) Set(name, role, value string, offer PassphraseFunc) error {
	_, err := v.SetUndoable(name, role, value, offer)
	return err
}

// SetUndoable is Set that also returns undo, which takes back this call alone: the role gets its previous
// value again, or is removed when it had none, unless another writer changed it since, and a pending entry
// this call queued is removed again. Undoing needs no passphrase: an encrypted, locked vault is only ever
// touched by removing the pending file this call wrote, and the key of any other is the one this process
// holds. undo is nil when err is not.
func (v *Vault) SetUndoable(name, role, value string, offer PassphraseFunc) (undo func() error, err error) {
	return v.SetUndoableWithID(name, "", role, value, offer)
}

// SetUndoableWithID is SetUndoable that also records credentialID, the config id of the credential, in the
// entry, or in the pending entry of a locked vault. An id the entry already has is kept; an empty
// credentialID records nothing.
func (v *Vault) SetUndoableWithID(name, credentialID, role, value string, offer PassphraseFunc) (undo func() error, err error) {
	state, err := v.State()
	if err != nil {
		return nil, err
	}
	switch state {
	case StateAbsent:
		passphrase := ""
		if offer != nil {
			passphrase, err = offer(createPrompt)
			if err != nil {
				return nil, err
			}
		}
		w, err := v.create(passphrase, name, credentialID, role, value)
		if errors.Is(err, errNotAbsent) {
			// Another process created the vault since State looked; the value goes into that one.
			return v.SetUndoableWithID(name, credentialID, role, value, nil)
		}
		if err != nil {
			return nil, err
		}
		return func() error { return v.undo(w) }, nil
	case StateUnencrypted, StateUnlocked:
		w, err := v.update(v.writeIdentity(state), updateOpts{}, func(doc *document) (bool, error) {
			doc.setRole(name, credentialID, role, value, time.Now().UTC())
			return true, nil
		})
		if err != nil {
			return nil, err
		}
		return func() error { return v.undo(w) }, nil
	case StateLocked:
		path, err := v.addPending(name, credentialID, map[string]string{role: value})
		if err != nil {
			return nil, err
		}
		return func() error {
			if err := os.Remove(path); err != nil && !isNotExist(err) {
				return err
			}
			return nil
		}, nil
	}
	return nil, fmt.Errorf("vault: unreachable state %q", state)
}

// RecordCredentialIDs records ids, the config id of each credential by name, in the vault entry of that name
// where the entry has none yet. It creates no entry and changes no secret. An encrypted, locked vault takes
// each id as a pending write instead, which needs no passphrase and is merged on the next unlock; an absent
// vault is left alone. It reports how many ids it recorded or queued, and undo, which takes back this call
// alone without a passphrase: byte for byte while nobody wrote the vault since, otherwise only what this call
// changed and nobody changed since. undo is nil when err is not.
func (v *Vault) RecordCredentialIDs(ids map[string]string) (recorded int, undo func() error, err error) {
	unchanged := func() error { return nil }
	state, err := v.State()
	if err != nil {
		return 0, nil, err
	}
	switch state {
	case StateAbsent:
		return 0, unchanged, nil
	case StateUnencrypted, StateUnlocked:
		count := 0
		w, err := v.update(v.writeIdentity(state), updateOpts{}, func(doc *document) (bool, error) {
			count = len(doc.recordCredentialIDs(ids))
			return count > 0, nil
		})
		if err != nil {
			return 0, nil, err
		}
		if w == nil {
			return 0, unchanged, nil
		}
		return count, func() error { return v.undo(w) }, nil
	case StateLocked:
		var written []string
		remove := func() error {
			var failed error
			for _, path := range written {
				if err := os.Remove(path); err != nil && !isNotExist(err) {
					failed = err
				}
			}
			return failed
		}
		names := make([]string, 0, len(ids))
		for name, id := range ids {
			if id != "" {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			path, err := v.addPending(name, ids[name], nil)
			if err != nil {
				_ = remove()
				return 0, nil, err
			}
			written = append(written, path)
		}
		return len(written), remove, nil
	}
	return 0, nil, fmt.Errorf("vault: unreachable state %q", state)
}

// Delete removes one credential role from the vault and reports whether it was there. Deleting from an
// encrypted, locked vault needs the passphrase the same way Get does, because the entry to remove is inside
// secrets.age.
func (v *Vault) Delete(name, role string, ask PassphraseFunc) (bool, error) {
	state, err := v.State()
	if err != nil {
		return false, err
	}
	switch state {
	case StateAbsent:
		return false, nil
	case StateLocked:
		if err := v.ensureUnlocked(ask); err != nil {
			return false, err
		}
		state = StateUnlocked
	}
	w, err := v.update(v.writeIdentity(state), updateOpts{}, func(doc *document) (bool, error) {
		return doc.deleteRole(name, role), nil
	})
	return w != nil, err
}

// ensureUnlocked opens a locked vault with the passphrase ask supplies, and caches the result. It is a
// no-op once the vault is already unlocked in this process.
func (v *Vault) ensureUnlocked(ask PassphraseFunc) error {
	if v.unlocked {
		return nil
	}
	if ask == nil {
		return ErrNoTerminal
	}
	passphrase, err := ask(unlockPrompt)
	if err != nil {
		return err
	}
	_, err = v.unlock(passphrase)
	return err
}

// Unlock opens an encrypted, locked vault with passphrase, merges every pending entry into it, and caches
// the result for the rest of this process. It reports how many pending entries were merged. Called on a
// vault that is not locked, it does nothing and reports zero: there is nothing to unlock.
func (v *Vault) Unlock(passphrase string) (merged int, err error) {
	state, err := v.State()
	if err != nil {
		return 0, err
	}
	if state != StateLocked {
		return 0, nil
	}
	return v.unlock(passphrase)
}

// Forget drops the identity and document this process holds in memory for an encrypted vault it unlocked,
// so a later read in this process asks for the passphrase again; it does nothing to a vault that is not
// unlocked here. It touches no file: a vault process outside this one, if it still holds the key, is
// unaffected and is locked separately (see vaultproc.LockProcess). This is as close as the vault API
// comes to undoing Unlock, since neither the key nor the decrypted document was ever written to disk to
// begin with.
func (v *Vault) Forget() {
	v.unlocked = false
	v.identity = nil
	v.doc = nil
}

func (v *Vault) unlock(passphrase string) (int, error) {
	id, err := v.readIdentity(passphrase)
	if err != nil {
		return 0, err
	}
	pending, err := v.pendingFiles()
	if err != nil {
		return 0, err
	}
	// Merging pending entries writes the document, so it reads the document under the write lock. Without
	// pending entries there is nothing to write and no lock to wait for.
	locked := len(pending) > 0
	if locked {
		release, err := v.lockWrites()
		if err != nil {
			return 0, err
		}
		defer release()
	}
	doc, consumed, err := v.openHeld(id)
	if err != nil {
		return 0, err
	}
	if len(consumed) > 0 && locked {
		if err := v.writeSecrets(id, doc); err != nil {
			return 0, err
		}
		for _, p := range consumed {
			// A file left behind merges again next time, a duplicate and no loss.
			_ = os.Remove(p.path)
		}
	}
	v.identity, v.doc, v.unlocked = id, doc, true
	return len(consumed), nil
}

// readIdentity reads key.age and recipient and opens the key with passphrase.
func (v *Vault) readIdentity(passphrase string) (*identity, error) {
	keyData, err := readFile(v.keyPath())
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", v.keyPath(), err)
	}
	recipientPEM, err := readFile(v.recipientPath())
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	return decryptKey(keyData, passphrase, recipientPEM)
}

// openHeld reads secrets.age with id and merges every pending entry into it, without removing one. A caller
// that writes the result holds the write lock and has checked that the vault is still encrypted.
func (v *Vault) openHeld(id *identity) (*document, []heldPending, error) {
	if state, err := v.diskState(); err != nil {
		return nil, nil, err
	} else if state != StateLocked {
		return nil, nil, errSwitched
	}
	doc, err := v.readSecrets(id.key)
	if err != nil {
		return nil, nil, err
	}
	consumed, err := v.mergePendingWhere(id, doc, nil)
	if err != nil {
		return nil, nil, err
	}
	return doc, consumed, nil
}

// create makes the vault's first entry, and with it the vault itself: unencrypted when passphrase is
// empty, encrypted to a freshly generated key otherwise. It fails with errNotAbsent when another process
// created the vault in the meantime, and writes nothing then.
func (v *Vault) create(passphrase, name, credentialID, role, value string) (*write, error) {
	release, err := v.lockWrites()
	if err != nil {
		return nil, err
	}
	defer release()
	if state, err := v.diskState(); err != nil {
		return nil, err
	} else if state != StateAbsent {
		return nil, errNotAbsent
	}
	doc := newDocument()
	doc.setRole(name, credentialID, role, value, time.Now().UTC())
	w := &write{base: newDocument(), result: doc.clone(), keepFile: true}
	v.journalSwitched()

	if passphrase == "" {
		w.path = v.plainPath()
		return w, v.writePlain(doc)
	}

	id, err := generateIdentity()
	if err != nil {
		return nil, err
	}
	keyData, err := encryptKey(id, passphrase)
	if err != nil {
		return nil, err
	}
	if err := writeFile(v.recipientPath(), []byte(id.recipient.String()+"\n")); err != nil {
		return nil, err
	}
	if err := writeFile(v.keyPath(), keyData); err != nil {
		return nil, err
	}
	if err := v.writeSecrets(id, doc); err != nil {
		return nil, err
	}
	v.identity, v.doc, v.unlocked = id, doc, true
	w.id, w.path = id, v.secretsPath()
	return w, nil
}

func (v *Vault) writePlain(doc *document) error {
	data, err := encodeDocument(doc)
	if err != nil {
		return fmt.Errorf("cannot encode %s: %w", v.plainPath(), err)
	}
	return writeFile(v.plainPath(), data)
}

func (v *Vault) writeSecrets(id *identity, doc *document) error {
	// Every regular write re-labels approvals granted under the legacy fingerprint.
	doc.upgradeFingerprints()
	plain, err := encodeDocument(doc)
	if err != nil {
		return fmt.Errorf("cannot encode %s: %w", v.secretsPath(), err)
	}
	data, err := encryptTo(string(plain), id.recipient)
	if err != nil {
		return err
	}
	return writeFile(v.secretsPath(), data)
}

func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
