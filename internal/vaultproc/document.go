package vaultproc

import (
	"encoding/json"
	"errors"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// document is what the vault process holds as the kit's document (kitvaultproc.Held.Document): every role
// of every credential, keyed by credential name and then by role, and the bindings a get is checked
// against. Its JSON is part of the handover between releases from this one on.
type document struct {
	Secrets  map[string]map[string][]byte `json:"secrets"`
	Bindings vault.Bindings               `json:"bindings"`
}

// wipe overwrites the values the document holds.
func (d *document) wipe() {
	for _, roles := range d.Secrets {
		for _, value := range roles {
			clear(value)
		}
	}
	d.Secrets = nil
}

// decodeDocument reads a held document. Its error never quotes the bytes. The caller wipes the result.
func decodeDocument(data []byte) (*document, error) {
	d := &document{}
	if err := json.Unmarshal(data, d); err != nil {
		d.wipe()
		return nil, errors.New("the vault process document cannot be read")
	}
	if d.Secrets == nil {
		d.Secrets = map[string]map[string][]byte{}
	}
	d.Bindings = copyBindings(d.Bindings)
	return d, nil
}

// encode returns the document's bytes. The caller clears them.
func (d *document) encode() ([]byte, error) {
	data, err := json.Marshal(d)
	if err != nil {
		return nil, errors.New("cannot encode the vault process document")
	}
	return data, nil
}

// documentOf returns the document of what snap holds. The caller wipes it.
func documentOf(secrets map[string]map[string]string, bindings vault.Bindings) *document {
	d := &document{Secrets: make(map[string]map[string][]byte, len(secrets)), Bindings: copyBindings(bindings)}
	for credential, roles := range secrets {
		d.Secrets[credential] = make(map[string][]byte, len(roles))
		for role, value := range roles {
			d.Secrets[credential][role] = []byte(value)
		}
	}
	return d
}

// copyBindings returns bindings with maps of their own, never nil.
func copyBindings(b vault.Bindings) vault.Bindings {
	out := vault.Bindings{IDs: make(map[string]string, len(b.IDs)), Approvals: make(map[string]string, len(b.Approvals))}
	for name, id := range b.IDs {
		out.IDs[name] = id
	}
	for name, fingerprint := range b.Approvals {
		out.Approvals[name] = fingerprint
	}
	return out
}

// decodeEarlier reads the handover of a vault process of a release before the kit's channel, up to protocol
// version 10 (v2.2.0 and the releases after the rename alike): the JSON of vault.Snapshot. It is
// kitvaultproc.RunOptions.DecodeEarlier, so it runs only in a successor and only where the kit's own format
// does not parse. A state without LocksAt is refused by the kit, so a handover never extends an unlock.
func decodeEarlier(data []byte) (kitvaultproc.State, error) {
	var snap vault.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil || snap.Identity == "" {
		return kitvaultproc.State{}, errors.New("the vault handed over cannot be read")
	}
	key, err := age.ParseX25519Identity(snap.Identity)
	snap.Identity = ""
	if err != nil {
		return kitvaultproc.State{}, errors.New("the vault key handed over cannot be read")
	}
	d := documentOf(snap.Secrets, snap.Bindings)
	snap.Secrets = nil
	defer d.wipe()
	doc, err := d.encode()
	if err != nil {
		return kitvaultproc.State{}, err
	}
	state := kitvaultproc.State{Identity: key, Document: doc, MaxUnlock: snap.MaxUnlock}
	if snap.LocksAt != nil {
		state.LocksAt = *snap.LocksAt
	}
	// A predecessor without the field had no maximum; the successor then counts from its own start.
	if snap.UnlockedAt != nil && !snap.UnlockedAt.After(time.Now()) {
		state.UnlockedAt = *snap.UnlockedAt
	}
	return state, nil
}
