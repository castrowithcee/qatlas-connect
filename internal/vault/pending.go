package vault

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// pendingPayload is what a pending entry decrypts to: the roles a locked Set added or changed for one
// credential name. It carries no entry id: the id a merge assigns, or reuses, is decided by matching Name
// against the vault's document, which is unreadable while the vault that wrote the pending entry was locked.
// CredentialID is the config id of the credential and is merged like the roles.
type pendingPayload struct {
	Name         string            `json:"name"`
	CredentialID string            `json:"credential_id,omitempty"`
	Roles        map[string]string `json:"roles"`
	Modified     time.Time         `json:"modified"`
}

// addPending queues roles for name as a pending entry, encrypted to the vault's public recipient alone, so it
// needs no passphrase. Without roles it carries only the credential id. It returns the entry's name.
func (v *Vault) addPending(name, credentialID string, roles map[string]string) (string, error) {
	payload := pendingPayload{Name: name, CredentialID: credentialID, Roles: roles, Modified: time.Now().UTC()}
	plain, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("cannot encode the pending entry for %s: %w", name, err)
	}
	defer clear(plain)
	return v.kv.AddPending(plain)
}

// decodePending decodes the pending entry name of the vault directory dir.
func decodePending(dir string, p kitvault.Pending) (pendingPayload, error) {
	var payload pendingPayload
	if err := json.Unmarshal(p.Payload, &payload); err != nil {
		return pendingPayload{}, fmt.Errorf("%s does not hold a valid pending entry",
			filepath.Join(dir, pendingDir, p.Name))
	}
	return payload, nil
}

// mergeFunc is the kit's merge of the vault directory dir: every pending entry is merged and counts as
// merged.
func mergeFunc(dir string) kitvault.MergeFunc {
	return func(data []byte, pending []kitvault.Pending) ([]byte, []string, error) {
		doc, err := decodeDocument(filepath.Join(dir, secretsFile), data)
		if err != nil {
			return nil, nil, err
		}
		done, err := doc.mergeEntries(dir, pending, nil)
		if err != nil {
			return nil, nil, err
		}
		// Every write of an encrypted document re-labels approvals granted under the legacy fingerprint.
		doc.upgradeFingerprints()
		merged, err := encodeDocument(filepath.Join(dir, secretsFile), doc)
		if err != nil {
			return nil, nil, err
		}
		return merged, done, nil
	}
}

// mergeEntries merges the pending entries of the vault directory dir whose payload keep accepts into d, in
// name order, a payload with only a credential id last, so it finds an entry a pending secret creates. keep
// may rename the payload before it is merged, or refuse the merge with an error; a nil keep accepts every
// entry. It returns the names of the entries it merged.
func (d *document) mergeEntries(dir string, pending []kitvault.Pending, keep func(p *pendingPayload) (bool, error)) (
	[]string, error) {
	var done []string
	var idsOnly []pendingPayload
	for _, p := range pending {
		payload, err := decodePending(dir, p)
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
		done = append(done, p.Name)
		if len(payload.Roles) == 0 {
			idsOnly = append(idsOnly, payload)
			continue
		}
		d.mergePending(payload)
	}
	for _, payload := range idsOnly {
		d.mergePending(payload)
	}
	return done, nil
}

// mergePending applies one pending payload to the document: an existing entry of the same name gets the
// payload's roles merged in, a name not yet present becomes a new entry. A payload with only a credential id
// never creates an entry and leaves the modification time alone, since no secret changed.
func (d *document) mergePending(p pendingPayload) {
	entry, ok := d.byName(p.Name)
	if !ok {
		if len(p.Roles) == 0 {
			return
		}
		entry = Entry{ID: newID(), Name: p.Name, Roles: map[string]string{}, Created: p.Modified}
	}
	if entry.CredentialID == "" {
		entry.CredentialID = p.CredentialID
	}
	if entry.Roles == nil {
		entry.Roles = map[string]string{}
	}
	for role, value := range p.Roles {
		entry.Roles[role] = value
	}
	if len(p.Roles) > 0 {
		entry.Modified = p.Modified
	}
	d.Entries[entry.ID] = entry
}
