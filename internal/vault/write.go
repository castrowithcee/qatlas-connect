package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"filippo.io/age"
)

// errSwitched reports a vault that another process switched between encrypted and unencrypted after this one
// looked at it. Nothing was written: the change would have landed beside the vault's real document.
var errSwitched = errors.New("vault: another process switched the vault between encrypted and unencrypted; try again")

// errNotAbsent tells create that another process created the vault in the meantime.
var errNotAbsent = errors.New("vault: the vault exists already")

// diskState is State from the files alone, whatever this process unlocked.
func (v *Vault) diskState() (State, error) {
	if exists, err := fileExists(v.plainPath()); err != nil {
		return "", err
	} else if exists {
		return StateUnencrypted, nil
	}
	if exists, err := fileExists(v.keyPath()); err != nil {
		return "", err
	} else if exists {
		return StateLocked, nil
	}
	return StateAbsent, nil
}

// writeIdentity is the identity a write of the document needs in state: none for an unencrypted vault, the one
// this process unlocked otherwise.
func (v *Vault) writeIdentity(state State) *identity {
	if state == StateUnencrypted {
		return nil
	}
	return v.identity
}

// write is one write of the document through update, kept so that an undo can take back this write alone.
type write struct {
	id       *identity // nil for an unencrypted vault
	path     string
	found    []byte // the file as the write found it, nil when it did not exist
	existed  bool
	wrote    []byte
	consumed []heldPending // pending files the write merged and removed
	foundDoc *document     // found, decoded, before any pending file was merged
	base     *document     // the document the change was applied to, pending files merged
	result   *document     // the document written
	// keepFile makes an undo set back only what the write changed, never the bytes it found: the write
	// created the vault, and the vault stays.
	keepFile bool
}

// updateOpts adjusts update for the one writer that needs it.
type updateOpts struct {
	// pending merges pending files into the document before the change and returns those it merged; nil merges
	// every one, as an unlock does.
	pending func(id *identity, doc *document) ([]heldPending, error)
	// strict fails and undoes the write when a merged pending file cannot be removed.
	strict bool
	// keep marks a write that a checkpoint's restore must not take back.
	keep bool
}

// update is the one way a change of the document reaches disk. Under the write lock it reads the document as
// it is on disk now, with id for an encrypted vault and pending files merged, or the unencrypted one for a nil
// id, applies change to it, writes it when change reports a change, and adopts it as what this process holds.
// So no write is based on a document read before the lock, and none overwrites what another process stored in
// the meantime. It returns the write for an undo, or nil when nothing was written.
func (v *Vault) update(id *identity, opts updateOpts, change func(doc *document) (bool, error)) (*write, error) {
	release, err := v.lockWrites()
	if err != nil {
		return nil, err
	}
	defer release()
	return v.updateHeld(id, opts, change)
}

// updateHeld is update for a caller that holds the write lock already.
func (v *Vault) updateHeld(id *identity, opts updateOpts, change func(doc *document) (bool, error)) (*write, error) {
	want, path := StateUnencrypted, v.plainPath()
	if id != nil {
		want, path = StateLocked, v.secretsPath()
	}
	if state, err := v.diskState(); err != nil {
		return nil, err
	} else if state != want {
		return nil, errSwitched
	}
	found, existed, err := readExisting(path)
	if err != nil {
		return nil, err
	}
	var doc *document
	if id == nil {
		doc, err = decodePlain(path, found)
	} else {
		doc, err = decodeSecrets(path, found, id.key)
	}
	if err != nil {
		return nil, err
	}
	v.journalSaw(path, found, existed)
	foundDoc := doc.clone()

	var consumed []heldPending
	if id != nil {
		doc.upgradeFingerprints()
		merge := opts.pending
		if merge == nil {
			merge = func(id *identity, doc *document) ([]heldPending, error) {
				return v.mergePendingWhere(id, doc, nil)
			}
		}
		if consumed, err = merge(id, doc); err != nil {
			return nil, err
		}
	}
	base := doc.clone()
	changed, err := change(doc)
	if err != nil {
		return nil, err
	}
	data, err := encodeDocument(doc)
	if err != nil {
		return nil, fmt.Errorf("cannot encode %s: %w", path, err)
	}
	if changed && len(consumed) == 0 {
		// A change that leaves the document as it was is no write, so it does not stand in the way of an
		// undo that puts back the very bytes an earlier write found.
		if same, err := encodeDocument(base); err == nil {
			changed = !bytes.Equal(same, data)
			clear(same)
		}
	}
	if !changed {
		clear(data)
		v.adopt(id, doc)
		return nil, nil
	}
	if id != nil {
		encrypted, err := encryptTo(string(data), id.recipient)
		clear(data)
		if err != nil {
			return nil, err
		}
		data = encrypted
	}
	if err := writeFile(path, data); err != nil {
		return nil, err
	}
	for i, p := range consumed {
		if err := os.Remove(p.path); err != nil && !isNotExist(err) && opts.strict {
			// A pending file left behind would merge again on the next unlock; the write is taken back.
			undoErr := putBack(path, found, existed, consumed[:i])
			if undoErr != nil {
				return nil, fmt.Errorf("cannot remove %s: %w; the vault could not be put back as it was: %v",
					p.path, err, undoErr)
			}
			return nil, fmt.Errorf("cannot remove %s: %w", p.path, err)
		}
	}
	w := &write{id: id, path: path, found: found, existed: existed, wrote: data, consumed: consumed,
		foundDoc: foundDoc, base: base, result: doc.clone()}
	v.journalWrote(w, opts.keep)
	v.adopt(id, doc)
	return w, nil
}

// adopt makes doc what this process holds of the encrypted vault id belongs to, when it holds that vault
// unlocked. An unencrypted vault is read from disk every time and holds nothing.
func (v *Vault) adopt(id *identity, doc *document) {
	if id == nil || !v.unlocked || v.identity == nil || v.identity.recipient.String() != id.recipient.String() {
		return
	}
	v.doc = doc
}

// undo takes back w alone, without a passphrase. While the file still holds what w wrote, it gets back the
// very bytes w found, and the pending files w merged come back too. Otherwise another write came after w, and
// only what w changed and nobody changed since is set back, so whatever another process stored stays.
func (v *Vault) undo(w *write) error {
	if w == nil {
		return nil
	}
	release, err := v.lockWrites()
	if err != nil {
		return err
	}
	defer release()
	current, present, err := readExisting(w.path)
	if err != nil {
		return err
	}
	if !w.keepFile && present && bytes.Equal(current, w.wrote) {
		if err := putBack(w.path, w.found, w.existed, w.consumed); err != nil {
			return err
		}
		v.journalUndid(w)
		v.adopt(w.id, w.foundDoc.clone())
		return nil
	}
	_, err = v.updateHeld(w.id, updateOpts{}, func(doc *document) (bool, error) {
		return doc.revert(w.base, w.result), nil
	})
	return err
}

// putBack writes found back to path, or removes path when it did not exist, and writes back the pending
// files in consumed.
func putBack(path string, found []byte, existed bool, consumed []heldPending) error {
	var failed []error
	if existed {
		failed = append(failed, writeFile(path, found))
	} else if err := os.Remove(path); err != nil && !isNotExist(err) {
		failed = append(failed, err)
	}
	for _, p := range consumed {
		failed = append(failed, writeFile(p.path, p.data))
	}
	return errors.Join(failed...)
}

// readExisting reads path; a missing file is no error but reports existed false.
func readExisting(path string) (data []byte, existed bool, err error) {
	data, err = readFile(path)
	switch {
	case err == nil:
		return data, true, nil
	case isNotExist(err):
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("cannot read %s: %w", path, err)
	}
}

// decodePlain decodes the bytes of secrets.json as loadPlainDocument does; nil is an empty document.
func decodePlain(path string, data []byte) (*document, error) {
	if data == nil {
		return newDocument(), nil
	}
	doc, err := decodeDocument(path, data)
	if err != nil {
		return nil, err
	}
	doc.Approvals = nil
	return doc, nil
}

// decodeSecrets decrypts and decodes the bytes of secrets.age; nil is an empty document.
func decodeSecrets(path string, data []byte, key age.Identity) (*document, error) {
	if data == nil {
		return newDocument(), nil
	}
	plain, err := decryptFrom(data, key)
	if err != nil {
		return nil, fmt.Errorf("cannot decrypt %s: %w", path, err)
	}
	defer clear(plain)
	return decodeDocument(path, plain)
}

// mergePendingWhere merges the pending files whose payload keep accepts into doc, in the order an unlock merges
// in, and returns them with their very bytes. keep may rename the payload before it is merged, or refuse the
// merge with an error; a nil keep accepts every file. No file is removed here: the caller removes them once
// the document that absorbed them is written.
func (v *Vault) mergePendingWhere(id *identity, doc *document, keep func(p *pendingPayload) (bool, error)) ([]heldPending, error) {
	names, err := v.pendingFiles()
	if err != nil {
		return nil, err
	}
	var held []heldPending
	// A payload with only a credential id goes last, so it finds an entry a pending secret creates.
	var idsOnly []pendingPayload
	for _, name := range names {
		payload, data, err := v.readPending(id, name)
		if err != nil {
			return nil, err
		}
		if keep != nil {
			if ok, err := keep(&payload); err != nil {
				return nil, err
			} else if !ok {
				continue
			}
		}
		held = append(held, heldPending{path: filepath.Join(v.pendingPath(), name), data: data})
		if len(payload.Roles) == 0 {
			idsOnly = append(idsOnly, payload)
			continue
		}
		doc.mergePending(payload)
	}
	for _, payload := range idsOnly {
		doc.mergePending(payload)
	}
	return held, nil
}

// revert sets back in d what changed from base to result wherever d still holds result's version, and reports
// whether it changed d. What another writer changed since stays as that writer left it, a role, a name or an
// approval alike.
func (d *document) revert(base, result *document) bool {
	changed := false
	now := time.Now().UTC()
	for _, id := range unionKeys(base.Entries, result.Entries) {
		b, inBase := base.Entries[id]
		a, inResult := result.Entries[id]
		c, inDoc := d.Entries[id]
		if !inResult {
			// The write removed the entry; it comes back unless it is back already or its name is taken.
			if inBase && !inDoc {
				if _, taken := d.byName(b.Name); !taken {
					d.Entries[id] = cloneEntry(b)
					changed = true
				}
			}
			continue
		}
		if !inDoc {
			continue
		}
		e := cloneEntry(c)
		touched, rolesTouched := false, false
		if inBase {
			if a.Name != b.Name && c.Name == a.Name {
				if other, taken := d.byName(b.Name); !taken || other.ID == id {
					e.Name, touched = b.Name, true
				}
			}
			if a.CredentialID != b.CredentialID && c.CredentialID == a.CredentialID {
				e.CredentialID, touched = b.CredentialID, true
			}
			if a.RenamedFrom != b.RenamedFrom && c.RenamedFrom == a.RenamedFrom {
				e.RenamedFrom, touched = b.RenamedFrom, true
			}
		}
		for _, role := range unionKeys(b.Roles, a.Roles) {
			bv, bok := b.Roles[role]
			av, aok := a.Roles[role]
			cv, cok := c.Roles[role]
			if bok == aok && bv == av || cok != aok || cv != av {
				continue
			}
			if bok {
				e.Roles[role] = bv
			} else {
				delete(e.Roles, role)
			}
			touched, rolesTouched = true, true
		}
		if !touched {
			continue
		}
		changed = true
		if len(e.Roles) == 0 {
			delete(d.Entries, id)
			continue
		}
		if rolesTouched {
			e.Modified = now
		}
		d.Entries[id] = e
	}
	for _, name := range unionKeys(base.Approvals, result.Approvals) {
		b, bok := base.Approvals[name]
		a, aok := result.Approvals[name]
		c, cok := d.Approvals[name]
		if sameApproval(b, bok, a, aok) || !sameApproval(c, cok, a, aok) {
			continue
		}
		if bok {
			if d.Approvals == nil {
				d.Approvals = map[string]Approval{}
			}
			d.Approvals[name] = b
		} else {
			delete(d.Approvals, name)
		}
		changed = true
	}
	return changed
}

func sameApproval(x Approval, xok bool, y Approval, yok bool) bool {
	if xok != yok {
		return false
	}
	if !xok {
		return true
	}
	a, errA := json.Marshal(x)
	b, errB := json.Marshal(y)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

func cloneEntry(e Entry) Entry {
	roles := make(map[string]string, len(e.Roles))
	for role, value := range e.Roles {
		roles[role] = value
	}
	e.Roles = roles
	return e
}

func unionKeys[V any](a, b map[string]V) []string {
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}
