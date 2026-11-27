package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// update is the one way a change of the document reaches disk. The kit hands it the document as it is on
// disk now, under the vault's write lock, so no write rests on a document read before the lock and none
// overwrites what another process stored meanwhile. The pending entries of an encrypted vault are merged
// first, as an unlock merges them. change reports whether it changed the document; nothing is written when
// it did not. update returns the kit's undo, which takes back this write alone, and whether it wrote.
func (v *Vault) update(change func(doc *document) (bool, error), options ...kitvault.UpdateOption) (
	undo func() error, wrote bool, err error) {
	return v.updatePending(func(doc *document, pending []kitvault.Pending) (bool, []string, error) {
		done, err := doc.mergeEntries(v.dir, pending, nil)
		if err != nil {
			return false, nil, err
		}
		changed, err := change(doc)
		return changed, done, err
	}, options...)
}

// updatePending is update for a change that picks the pending entries it merges itself and reports their
// names.
func (v *Vault) updatePending(change func(doc *document, pending []kitvault.Pending) (bool, []string, error),
	options ...kitvault.UpdateOption) (undo func() error, wrote bool, err error) {
	state, err := v.kv.State()
	if err != nil {
		return nil, false, err
	}
	// The kit refuses the write when another process switched the vault since.
	encrypted := state == StateUnlocked
	path := v.plainPath()
	if encrypted {
		path = v.secretsPath()
	}
	undo, err = v.kv.UpdateWithOptions(func(data []byte, pending []kitvault.Pending) ([]byte, []string, error) {
		doc, err := decodeDocument(path, data)
		if err != nil {
			return nil, nil, err
		}
		if encrypted {
			// Every write of an encrypted document re-labels approvals granted under the legacy fingerprint.
			doc.upgradeFingerprints()
		} else {
			doc.Approvals = nil
		}
		before, err := encodeDocument(path, doc)
		if err != nil {
			return nil, nil, err
		}
		defer clear(before)
		changed, done, err := change(doc, pending)
		if err != nil {
			return nil, nil, err
		}
		if !changed {
			return nil, nil, kitvault.ErrNoChange
		}
		after, err := encodeDocument(path, doc)
		if err != nil {
			return nil, nil, err
		}
		// A change that leaves the document as it was is no write, so it does not stand in the way of an
		// undo that puts back the very bytes an earlier write found.
		if len(done) == 0 && bytes.Equal(before, after) {
			clear(after)
			return nil, nil, kitvault.ErrNoChange
		}
		wrote = true
		return after, done, nil
	}, options...)
	if err != nil {
		return nil, false, mapDocumentErr(err)
	}
	return undo, wrote, nil
}

// revertFunc is the kit's Revert of the vault directory dir: it takes one write of this process out of a
// document another process wrote since.
func revertFunc(dir string) kitvault.RevertFunc {
	return func(before, after, current []byte) ([]byte, error) {
		// The kit holds the write lock, so no switch of the mode can come between this look and the write.
		path, plain := filepath.Join(dir, plainFile), true
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			path, plain = filepath.Join(dir, secretsFile), false
		} else if err != nil {
			return nil, err
		}
		docs := make([]*document, 0, 3)
		for _, data := range [][]byte{before, after, current} {
			doc, err := decodeDocument(path, data)
			if err != nil {
				return nil, err
			}
			if plain {
				doc.Approvals = nil
			}
			docs = append(docs, doc)
		}
		// The write re-labelled legacy fingerprints; that is no change of an approval to set back.
		docs[0].upgradeFingerprints()
		if !docs[2].revert(docs[0], docs[1]) {
			return current, nil
		}
		return encodeDocument(path, docs[2])
	}
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
