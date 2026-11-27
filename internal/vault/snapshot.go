package vault

import "time"

// Snapshot is what an unlocked vault holds, handed to the vault process that keeps it open for later
// invocations: the vault's own key and the decrypted secrets, keyed by credential name and then by role.
//
// Its JSON is what a vault process reads from its handover pipe, and a vault process of one release hands it
// to its successor of the next (see vaultproc.Client.PrepareHandover): the field names below are a contract
// between consecutive releases, and a change keeps what an older release writes readable.
type Snapshot struct {
	// Identity is the vault's private key as age text. It is as secret as the secrets it decrypts.
	Identity string `json:"identity"`
	// Secrets holds every role of every credential the vault holds.
	Secrets map[string]map[string]string `json:"secrets"`
	// Bindings says which connection may read which credential; see Bindings.
	Bindings Bindings `json:"bindings"`
	// LocksAt is when a vault process handing the vault over to a successor would have locked itself, which
	// the successor keeps; nil for an unlock, after which the idle timeout runs from the start.
	LocksAt *time.Time `json:"locks_at,omitempty"`
	// UnlockedAt is when the vault was unlocked, which a successor counts its maximum unlock duration from.
	// Nil from a release that had no maximum: the successor then counts from its own start.
	UnlockedAt *time.Time `json:"unlocked_at,omitempty"`
	// MaxUnlock is the maximum unlock duration the handing-over process held, so a successor whose settings
	// cannot be trusted or were switched off keeps it; zero for none.
	MaxUnlock time.Duration `json:"max_unlock,omitempty"`
}

// Snapshot returns what this process unlocked, pending entries already merged in by Unlock. It fails with
// ErrNotUnlocked unless the vault was unlocked, or created encrypted, in this process.
func (v *Vault) Snapshot() (Snapshot, error) {
	snap, err := v.kv.Snapshot()
	if err != nil {
		return Snapshot{}, ErrNotUnlocked
	}
	defer clear(snap.Document)
	doc, err := decodeDocument(v.secretsPath(), snap.Document)
	if err != nil {
		return Snapshot{}, err
	}
	secrets := make(map[string]map[string]string, len(doc.Entries))
	for _, entry := range doc.Entries {
		secrets[entry.Name] = entry.Roles
	}
	return Snapshot{Identity: snap.Identity.String(), Secrets: secrets, Bindings: doc.bindings()}, nil
}
