package vault

import (
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
	case StateLocked:
		if err := v.ensureUnlocked(ask); err != nil {
			return "", false, StateLocked, err
		}
		state = StateUnlocked
	}
	doc, err := v.load()
	if err != nil {
		return "", false, state, err
	}
	value, found = roleOf(doc, name, role)
	return value, found, state, nil
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

// SetUndoable is Set that also returns undo, which puts the vault back as this call found it: the role gets
// its previous value again, or is removed when it had none, and a pending entry this call queued is removed
// again. Undoing needs no passphrase: an encrypted, locked vault is only ever touched by removing the
// pending entry this call wrote, and a vault this call created is unlocked in this process. undo is nil when
// err is not.
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
		if err := v.create(passphrase, name, credentialID, role, value); err != nil {
			return nil, err
		}
		return func() error {
			_, err := v.Delete(name, role, nil)
			return err
		}, nil
	case StateLocked:
		pending, err := v.addPending(name, credentialID, map[string]string{role: value})
		if err != nil {
			return nil, err
		}
		return func() error { return v.kv.RemovePending(pending) }, nil
	}
	doc, err := v.load()
	if err != nil {
		return nil, err
	}
	old, had := roleOf(doc, name, role)
	doc.setRole(name, credentialID, role, value, time.Now().UTC())
	if err := v.write(doc); err != nil {
		return nil, err
	}
	return func() error {
		doc, err := v.load()
		if err != nil {
			return err
		}
		restoreRole(doc, name, role, old, had)
		return v.write(doc)
	}, nil
}

// RecordCredentialIDs records ids, the config id of each credential by name, in the vault entry of that name
// where the entry has none yet. It creates no entry and changes no secret. An encrypted, locked vault takes
// each id as a pending write instead, which needs no passphrase and is merged on the next unlock; an absent
// vault is left alone. It reports how many ids it recorded or queued, and undo, which puts the vault back as
// this call found it without a passphrase: the document file byte for byte, or the pending entries it queued
// removed. undo is nil when err is not.
func (v *Vault) RecordCredentialIDs(ids map[string]string) (recorded int, undo func() error, err error) {
	unchanged := func() error { return nil }
	state, err := v.State()
	if err != nil {
		return 0, nil, err
	}
	switch state {
	case StateAbsent:
		return 0, unchanged, nil
	case StateLocked:
		var written []string
		remove := func() error {
			var failed error
			for _, name := range written {
				if err := v.kv.RemovePending(name); err != nil {
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
			pending, err := v.addPending(name, ids[name], nil)
			if err != nil {
				_ = remove()
				return 0, nil, err
			}
			written = append(written, pending)
		}
		return len(written), remove, nil
	}
	doc, err := v.load()
	if err != nil {
		return 0, nil, err
	}
	changed := doc.recordCredentialIDs(ids)
	if len(changed) == 0 {
		return 0, unchanged, nil
	}
	undo, err = v.writeWithUndo(doc)
	if err != nil {
		return 0, nil, err
	}
	return len(changed), undo, nil
}

// restoreRole sets a role back to the value it had, or removes it when it had none.
func restoreRole(doc *document, name, role, old string, had bool) {
	if had {
		doc.setRole(name, "", role, old, time.Now().UTC())
		return
	}
	doc.deleteRole(name, role)
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
	}
	doc, err := v.load()
	if err != nil {
		return false, err
	}
	if !doc.deleteRole(name, role) {
		return false, nil
	}
	return true, v.write(doc)
}

// ensureUnlocked opens a locked vault with the passphrase ask supplies, and caches the result. It is a
// no-op once the vault is already unlocked in this process.
func (v *Vault) ensureUnlocked(ask PassphraseFunc) error {
	if state, err := v.State(); err != nil || state == StateUnlocked {
		return err
	}
	if ask == nil {
		return ErrNoTerminal
	}
	passphrase, err := ask(unlockPrompt)
	if err != nil {
		return err
	}
	_, err = v.kv.Unlock(passphrase)
	return mapDocumentErr(err)
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
	merged, err = v.kv.Unlock(passphrase)
	return merged, mapDocumentErr(err)
}

// create makes the vault's first entry, and with it the vault itself: unencrypted when passphrase is
// empty, encrypted to a freshly generated key otherwise.
func (v *Vault) create(passphrase, name, credentialID, role, value string) error {
	doc := newDocument()
	doc.setRole(name, credentialID, role, value, time.Now().UTC())
	data, err := encodeDocument(v.secretsPath(), doc)
	if err != nil {
		return err
	}
	defer clear(data)
	if passphrase == "" {
		return v.kv.CreateUnencrypted(data)
	}
	return v.kv.Create(passphrase, data)
}
