package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
)

// Checkpoint records the files a write of secrets or approvals changes - the document, encrypted or not, the
// key and the recipient - together with what this process holds of the vault, and returns restore and done.
// It is for a change of several writes through this Vault that, when it fails, must be taken back whole.
//
// restore takes back what this Vault wrote since the checkpoint, and only that. While no other process wrote
// the document in the meantime, it puts the very bytes back, with every pending entry this Vault merged,
// removes such a file that was not there and a pending entry this Vault queued, and returns this Vault to the
// state it had. Otherwise it sets back what this Vault changed wherever nobody changed it since, so a value
// another process stored meanwhile, such as a rotated refresh token, stays. A checkpoint across the creation
// of the vault or a switch between encrypted and unencrypted always puts the bytes back. A vault directory the
// checkpoint did not find is removed again once it holds nothing else. restore needs no passphrase.
//
// done ends the checkpoint once the change is kept, and does nothing after restore; restore must not be called
// after done. Agent tokens and settings are not covered.
func (v *Vault) Checkpoint() (restore func() error, done func(), err error) {
	if v.journal != nil {
		return nil, nil, errors.New("vault: a checkpoint of this vault is open already")
	}
	files := []string{v.plainPath(), v.secretsPath(), v.keyPath(), v.recipientPath()}
	saved := map[string]fileState{}
	for _, path := range files {
		data, existed, err := readExisting(path)
		if err != nil {
			return nil, nil, err
		}
		saved[path] = fileState{data: data, existed: existed}
	}
	dirExisted, err := fileExists(v.dir)
	if err != nil {
		return nil, nil, err
	}
	pendingExisted, err := fileExists(v.pendingPath())
	if err != nil {
		return nil, nil, err
	}
	unlocked, id, doc := v.unlocked, v.identity, cloneDocument(v.doc)

	j := &journal{seen: map[string]fileState{}, clean: true}
	for path, state := range saved {
		j.seen[path] = state
	}
	v.journal = j
	done = func() {
		if v.journal == j {
			v.journal = nil
		}
	}
	restore = func() error {
		if v.journal != j {
			return errors.New("vault: the checkpoint has ended, there is nothing to restore")
		}
		v.journal = nil
		failed := v.restoreCheckpoint(j, files, saved, pendingExisted, unlocked, id, doc)
		if !dirExisted {
			failed = append(failed, v.removeEmptyDir())
		}
		return errors.Join(failed...)
	}
	return restore, done, nil
}

func (v *Vault) restoreCheckpoint(j *journal, files []string, saved map[string]fileState, pendingExisted, unlocked bool,
	id *identity, doc *document) []error {
	release, err := v.lockWrites()
	if err != nil {
		return []error{err}
	}
	defer release()
	var failed []error
	intact := j.clean
	for _, path := range []string{v.plainPath(), v.secretsPath()} {
		data, existed, err := readExisting(path)
		if err != nil {
			return []error{err}
		}
		if seen := j.seen[path]; existed != seen.existed || !bytes.Equal(data, seen.data) {
			intact = false
		}
	}

	switch {
	case intact || j.switched:
		for _, path := range files {
			if state := saved[path]; state.existed {
				failed = append(failed, writeFile(path, state.data))
			} else if err := os.Remove(path); err != nil && !isNotExist(err) {
				failed = append(failed, err)
			}
		}
		for _, w := range j.writes {
			for _, p := range w.consumed {
				failed = append(failed, writeFile(p.path, p.data))
			}
		}
		v.unlocked, v.identity, v.doc = unlocked, id, nil
		if unlocked && id != nil {
			restored, err := decodeSecrets(v.secretsPath(), saved[v.secretsPath()].data, id.key)
			if err != nil {
				restored, failed = cloneDocument(doc), append(failed, err)
			}
			v.doc = restored
		}
	case len(j.writes) > 0:
		last := j.writes[len(j.writes)-1]
		v.unlocked, v.identity = unlocked, id
		_, err := v.updateHeld(last.id, updateOpts{}, func(current *document) (bool, error) {
			changed := false
			for i := len(j.writes) - 1; i >= 0; i-- {
				if current.revert(j.writes[i].base, j.writes[i].result) {
					changed = true
				}
			}
			return changed, nil
		})
		failed = append(failed, err)
		if !unlocked {
			v.doc = nil
		}
	default:
		// This Vault wrote nothing since the checkpoint; what other processes wrote stays.
		v.unlocked, v.identity, v.doc = unlocked, id, cloneDocument(doc)
	}

	for _, path := range j.pending {
		if err := os.Remove(path); err != nil && !isNotExist(err) {
			failed = append(failed, err)
		}
	}
	if !pendingExisted {
		if names, err := os.ReadDir(v.pendingPath()); err == nil && len(names) == 0 {
			if err := os.Remove(v.pendingPath()); err != nil && !isNotExist(err) {
				failed = append(failed, err)
			}
		}
	}
	return failed
}

// removeEmptyDir removes the vault directory once it holds nothing but the write lock file. It runs after the
// lock is released, since an open file cannot be removed everywhere.
func (v *Vault) removeEmptyDir() error {
	entries, err := os.ReadDir(v.dir)
	if isNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 1 && entries[0].Name() == writeLockFile {
		if err := os.Remove(filepath.Join(v.dir, writeLockFile)); err != nil && !isNotExist(err) {
			return err
		}
	}
	if err := os.Remove(v.dir); err != nil && !isNotExist(err) {
		return err
	}
	return nil
}

// fileState is a file's bytes and whether it existed.
type fileState struct {
	data    []byte
	existed bool
}

// journal is what an open checkpoint knows of the writes of this Vault since it was taken.
type journal struct {
	// seen holds each document file as this Vault last saw or left it.
	seen map[string]fileState
	// writes are the writes of the document a restore takes back, oldest first.
	writes []*write
	// pending are the pending entries this Vault queued.
	pending []string
	// clean stays set while no other writer touched the document since the checkpoint.
	clean bool
	// switched is set by a write that created the vault, changed its passphrase or switched its encryption.
	switched bool
}

func (v *Vault) journalSaw(path string, data []byte, existed bool) {
	j := v.journal
	if j == nil {
		return
	}
	if seen, ok := j.seen[path]; !ok || seen.existed != existed || !bytes.Equal(seen.data, data) {
		j.clean = false
	}
}

func (v *Vault) journalWrote(w *write, keep bool) {
	j := v.journal
	if j == nil {
		return
	}
	j.seen[w.path] = fileState{data: w.wrote, existed: true}
	if keep {
		// A write that must stay is no longer covered by putting the bytes back.
		j.clean = false
		return
	}
	j.writes = append(j.writes, w)
}

func (v *Vault) journalUndid(w *write) {
	j := v.journal
	if j == nil {
		return
	}
	j.seen[w.path] = fileState{data: w.found, existed: w.existed}
	j.writes = slices.DeleteFunc(j.writes, func(x *write) bool { return x == w })
}

func (v *Vault) journalSwitched() {
	if v.journal != nil {
		v.journal.switched = true
	}
}

func (v *Vault) journalPending(path string) {
	if v.journal != nil {
		v.journal.pending = append(v.journal.pending, path)
	}
}

// cloneDocument is document.clone that also takes nil, the document of a vault not unlocked here.
func cloneDocument(d *document) *document {
	if d == nil {
		return nil
	}
	return d.clone()
}
