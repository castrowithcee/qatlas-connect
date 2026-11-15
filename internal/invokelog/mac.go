package invokelog

import (
	"errors"

	"filippo.io/age"
	"github.com/castrowithcee/qatlas-kit/auditlog"
)

// keyLabel is the HKDF context the log key is derived with, so the log key is never the vault's key
// itself, nor any key the vault derives for another purpose. Every entry is signed under it.
const keyLabel = "qatlas-connect invocation log v1"

// legacyKeyLabel is the context entries were signed under before keyLabel. It only checks those entries and
// never signs one. Transitional until the next major release.
const legacyKeyLabel = "qatlas invocation log v1"

// currentMACKey is the key ID that names keyLabel as mac_key in a signed line; a signed line without
// mac_key names key ID zero, legacyKeyLabel.
const currentMACKey = 2

// Key is the log key, derived from the vault's own key; see DeriveKey. It signs entries and checks the
// check values of stored lines, and it never hands out its bytes. A nil Key signs nothing.
type Key = auditlog.Key

// Checker checks the check values of stored lines. A *Key is one; the vault process offers another that
// answers without ever handing out the key.
type Checker = auditlog.Checker

// DeriveKey returns the log key of the vault whose private key is id, with the age text of id as the
// secret, signing under keyLabel and checking older entries under legacyKeyLabel as well. The same vault
// key always yields the same log key, in the vault process and in a process that unlocked the vault itself
// alike; another vault key, a new one after 'vault encrypt' included, yields another.
func DeriveKey(id *age.X25519Identity) (*Key, error) {
	if id == nil {
		return nil, errors.New("no vault key to derive the log key from")
	}
	secret := []byte(id.String())
	defer clear(secret)
	return auditlog.DeriveKeys(secret, auditlog.KeyLabel{Label: keyLabel, ID: currentMACKey},
		auditlog.KeyLabel{Label: legacyKeyLabel})
}
