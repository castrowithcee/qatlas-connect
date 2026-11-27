package vault

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// Entry is one credential's secrets in the vault. Id is random and fixed for the life of the entry, so a
// pending write can be merged into the entry it belongs to by name even though the id was chosen without
// seeing the vault's content. Name is the credential name it belongs to; Roles holds one secret per role.
// CredentialID is the stable id of the credential in config.yaml, not the entry's own ID and not part of any
// fingerprint. It is set once, never replaced, and an older build ignores it. RenamedFrom is the name the
// entry had before a credential rename that is not yet committed to config.yaml; the run that renames the
// entry sets it and removes it once the configuration is saved. It is a hint about the direction of a rename,
// never a proof, and an older build ignores it. A vault entry carries no configuration value beyond the name,
// that id and that mark: everything else a credential needs, such as its provider or its type, stays in
// config.yaml.
type Entry struct {
	ID           string            `json:"id"`
	CredentialID string            `json:"credential_id,omitempty"`
	RenamedFrom  string            `json:"renamed_from,omitempty"`
	Name         string            `json:"name"`
	Roles        map[string]string `json:"roles"`
	Created      time.Time         `json:"created"`
	Modified     time.Time         `json:"modified"`
}

// document is the JSON shape held by secrets.json and, encrypted, by secrets.age. Approvals, by connection
// name, exist only in secrets.age: they sit in the same encrypted document as the secrets they release, so
// nobody who can merely write files, and encrypt to the public recipient, can add one without also replacing
// every secret. An unencrypted vault binds no connection and never keeps them.
type document struct {
	Schema    int                 `json:"schema"`
	Entries   map[string]Entry    `json:"entries,omitempty"`
	Approvals map[string]Approval `json:"approvals,omitempty"`
}

func newDocument() *document {
	return &document{Schema: schemaVersion, Entries: map[string]Entry{}}
}

// byName returns the entry of one credential and whether it exists.
func (d *document) byName(name string) (Entry, bool) {
	for _, entry := range d.Entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return Entry{}, false
}

// setRole stores one role's secret under the entry named name, creating the entry when it does not exist
// yet. now is passed in so a test can make the metadata deterministic.
func (d *document) setRole(name, credentialID, role, value string, now time.Time) {
	entry, ok := d.byName(name)
	if !ok {
		entry = Entry{ID: newID(), Name: name, Roles: map[string]string{}, Created: now}
	}
	if entry.CredentialID == "" {
		entry.CredentialID = credentialID
	}
	if entry.Roles == nil {
		entry.Roles = map[string]string{}
	}
	entry.Roles[role] = value
	entry.Modified = now
	d.Entries[entry.ID] = entry
}

// recordCredentialIDs sets the credential id of every entry whose name ids maps and that has none yet, and
// returns the ids of the entries it changed.
func (d *document) recordCredentialIDs(ids map[string]string) []string {
	var changed []string
	for id, entry := range d.Entries {
		if credentialID := ids[entry.Name]; credentialID != "" && entry.CredentialID == "" {
			entry.CredentialID = credentialID
			d.Entries[id] = entry
			changed = append(changed, id)
		}
	}
	return changed
}

// deleteRole removes one role from the entry named name and reports whether it was there. An entry left
// without any role is removed entirely, so an empty entry never lingers in the document.
func (d *document) deleteRole(name, role string) bool {
	entry, ok := d.byName(name)
	if !ok {
		return false
	}
	if _, ok := entry.Roles[role]; !ok {
		return false
	}
	delete(entry.Roles, role)
	if len(entry.Roles) == 0 {
		delete(d.Entries, entry.ID)
		return true
	}
	entry.Modified = time.Now().UTC()
	d.Entries[entry.ID] = entry
	return true
}

// newID returns a random, URL-safe entry id. It is 16 bytes of randomness, the same size a UUID spends on
// entropy, encoded as hex so it needs no further escaping anywhere it is used, such as a pending file name.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read only fails when the OS random source is unusable, which leaves nothing this
		// process could do safely; the same assumption every other user of crypto/rand in this module makes.
		panic("vault: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// decodeDocument decodes a document the kit read from path and checked for its schema. Nothing, the
// document of a vault without a document file, is an empty document. A document that does not decode wraps
// the kit's ErrDamaged, one of another schema its ErrUnsupportedSchema, with the messages this package always
// used.
func decodeDocument(path string, data []byte) (*document, error) {
	if len(data) == 0 {
		return newDocument(), nil
	}
	var d document
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, &documentError{msg: fmt.Sprintf("cannot read %s: not a valid vault document", path),
			err: kitvault.ErrDamaged}
	}
	// A document of a schema version this build does not know is refused rather than read or, worse,
	// silently overwritten with the current schema on the next write.
	if d.Schema != schemaVersion {
		return nil, &documentError{msg: fmt.Sprintf("cannot read %s: unsupported vault schema version %d, want %d",
			path, d.Schema, schemaVersion), err: kitvault.ErrUnsupportedSchema}
	}
	if d.Entries == nil {
		d.Entries = map[string]Entry{}
	}
	return &d, nil
}

func encodeDocument(path string, d *document) ([]byte, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot encode %s: %w", path, err)
	}
	return data, nil
}

// withoutApprovals is the kit's switch filter: approvals bind connections only while the vault is
// encrypted, so neither direction of a switch carries them over.
func withoutApprovals(data []byte) ([]byte, error) {
	doc, err := decodeDocument(plainFile, data)
	if err != nil {
		return nil, err
	}
	doc.Approvals = nil
	return encodeDocument(plainFile, doc)
}
