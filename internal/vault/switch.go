package vault

import (
	"errors"
	"os"

	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// ErrAlreadyEncrypted reports that Encrypt was called on a vault that already is one.
var ErrAlreadyEncrypted = errors.New("the vault is already encrypted")

// Encrypt turns an absent or unencrypted vault into an encrypted one: it generates a fresh key pair, wraps
// the private key with passphrase, and encrypts whatever document already exists, or an empty one for a
// vault that did not exist yet. The plaintext document is removed only once the encrypted one is complete.
// Called on a vault that is already encrypted, it changes nothing and reports ErrAlreadyEncrypted; 'qconnect
// vault passphrase' is the way to change the passphrase of one.
func (v *Vault) Encrypt(passphrase string) error {
	if passphrase == "" {
		return ErrEmptyPassphrase
	}
	state, err := v.State()
	if err != nil {
		return err
	}
	switch state {
	case StateLocked, StateUnlocked:
		return ErrAlreadyEncrypted
	case StateAbsent:
		data, err := encodeDocument(v.secretsPath(), newDocument())
		if err != nil {
			return err
		}
		return v.kv.Create(passphrase, data)
	}
	return mapDocumentErr(v.kv.Encrypt(passphrase))
}

// requireEncrypted fails with ErrNotEncrypted for a vault that has no passphrase.
func (v *Vault) requireEncrypted() error {
	state, err := v.State()
	if err != nil {
		return err
	}
	if state != StateLocked && state != StateUnlocked {
		return ErrNotEncrypted
	}
	return nil
}

// ChangePassphrase wraps the vault's key with a new passphrase after verifying the old one fresh against
// key.age, never from a passphrase cached earlier in the process. The key pair, the recipient and every
// stored secret stay as they were: only key.age changes.
func (v *Vault) ChangePassphrase(oldPassphrase, newPassphrase string) error {
	if newPassphrase == "" {
		return ErrEmptyPassphrase
	}
	state, err := v.State()
	if err != nil {
		return err
	}
	if state != StateLocked && state != StateUnlocked {
		return ErrNotEncrypted
	}
	if err := v.kv.ChangePassphrase(oldPassphrase, newPassphrase); err != nil {
		return err
	}
	if state == StateUnlocked {
		// The key pair stays, so this process keeps it and takes the document as it is on disk now: an
		// update that changes nothing still holds what it read under the write lock.
		_, _ = v.kv.Update(func([]byte, []kitvault.Pending) ([]byte, []string, error) {
			return nil, nil, kitvault.ErrNoChange
		})
	}
	return nil
}

// Decrypt turns an encrypted vault back into an unencrypted one, after merging every pending entry so
// nothing queued while it was locked is lost. Its approvals, its agent tokens, and its settings are dropped.
// passphrase is verified fresh against key.age. Called on a vault that is not encrypted, it changes nothing
// and reports ErrNotEncrypted.
func (v *Vault) Decrypt(passphrase string) error {
	if err := v.requireEncrypted(); err != nil {
		return err
	}
	// The kit refuses a vault without secrets.age, while this package always turned one into an empty
	// unencrypted vault. The empty document is written under the write lock only while the file is still
	// missing.
	if _, err := os.Stat(v.secretsPath()); errors.Is(err, os.ErrNotExist) {
		if _, err := v.kv.Unlock(passphrase); err != nil {
			return mapDocumentErr(err)
		}
		_, err := v.kv.Update(func(data []byte, _ []kitvault.Pending) ([]byte, []string, error) {
			if data != nil {
				return nil, nil, kitvault.ErrNoChange
			}
			empty, err := encodeDocument(v.secretsPath(), newDocument())
			return empty, nil, err
		})
		if err != nil {
			return mapDocumentErr(err)
		}
	}
	return mapDocumentErr(v.kv.Decrypt(passphrase))
}

// VerifyPassphrase checks passphrase against the vault's key.age and recipient without any side effect: no
// document is read, no pending entry is merged, and what this process holds is neither consulted nor
// replaced. Called on a vault that is not encrypted, it reports ErrNotEncrypted.
func (v *Vault) VerifyPassphrase(passphrase string) error {
	if err := v.requireEncrypted(); err != nil {
		return err
	}
	return v.kv.VerifyPassphrase(passphrase)
}
