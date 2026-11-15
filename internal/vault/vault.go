// Package vault stores credential secrets on disk next to the configuration, either unencrypted or
// encrypted to a passphrase. It is the alternative to the system keyring for a machine that has none, and
// the way a credential of type vault is filled and read.
//
// The mechanics, sealing, check values, locking, key derivation and pending entries, are those of the kit
// vault; this package holds what Connect keeps in it: its file names, the document of credential entries
// and approvals, how pending writes merge, the settings and the agent tokens.
//
// A vault lives in a directory named vault beside config.yaml. Unencrypted, it holds one file,
// secrets.json, a plain JSON document. Encrypted, it holds key.age, the vault's own X25519 private key
// wrapped with a passphrase; recipient, the matching public key; and secrets.age, the document encrypted to
// that public key. Because recipient is public, a new or changed entry can always be added without the
// passphrase: it is written to pending/<id>.age and merged into secrets.age the next time the vault is
// unlocked.
//
// An encrypted vault can also hold tokens.age, the agent tokens (see Token), and settings.age, its settings
// (see Settings). Both carry a check value keyed from the vault's private key, so a document merely
// encrypted to the public recipient is not trusted.
package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// DirName is the vault directory beside config.yaml.
const DirName = "vault"

// schemaVersion is the version of the JSON document a vault holds, encrypted or not.
const schemaVersion = 1

// Names of the files and the directory a vault is made of.
const (
	keyFile       = "key.age"
	recipientFile = "recipient"
	secretsFile   = "secrets.age"
	plainFile     = "secrets.json"
	pendingDir    = "pending"
)

// emptyDocument is what a secrets file of 0 bytes reads as.
var emptyDocument = []byte(`{"schema":1}`)

// State is what a vault currently is, so a caller can act on it without reading its files itself.
type State = kitvault.State

// The states a vault can be in.
const (
	// StateAbsent means no vault directory exists yet; the first Set creates one.
	StateAbsent = kitvault.StateAbsent
	// StateUnencrypted means the vault holds its document as plain JSON, without a passphrase.
	StateUnencrypted = kitvault.StateUnencrypted
	// StateLocked means the vault is encrypted and its identity has not been unlocked in this process.
	StateLocked = kitvault.StateLocked
	// StateUnlocked means the vault is encrypted and was unlocked earlier in this process.
	StateUnlocked = kitvault.StateUnlocked
)

// Errors of the kit vault this package passes on unchanged.
var (
	// ErrNoTerminal is returned by a PassphraseFunc that could not ask for a passphrase.
	ErrNoTerminal = kitvault.ErrNoTerminal
	// ErrWrongPassphrase reports a passphrase that does not unlock the vault's key.
	ErrWrongPassphrase = kitvault.ErrWrongPassphrase
	// ErrNotEncrypted reports an operation that needs an encrypted vault.
	ErrNotEncrypted = kitvault.ErrNotEncrypted
	// ErrEmptyPassphrase reports a passphrase of "".
	ErrEmptyPassphrase = kitvault.ErrEmptyPassphrase
)

// PermissionError reports a vault file or directory that other users can reach.
type PermissionError = kitvault.PermissionError

// ErrNotUnlocked reports a vault that was not unlocked in this process.
var ErrNotUnlocked = errors.New("the vault is not unlocked in this process")

// PassphraseFunc supplies the passphrase an encrypted vault needs to unlock, given the prompt to show. It
// returns ErrNoTerminal when it cannot ask. The kit's ReadPassphrase is the interactive implementation.
type PassphraseFunc func(prompt string) (string, error)

// Vault is one vault directory. The zero value is not usable; use New.
type Vault struct {
	dir string
	kv  *kitvault.Vault
}

// New returns the vault below dir, the directory holding config.yaml. The vault does not have to exist.
func New(dir string) *Vault { return newAt(filepath.Join(dir, DirName)) }

// newAt returns the vault in the vault directory itself.
func newAt(vaultDir string) *Vault {
	kv, err := kitvault.New(options(vaultDir))
	if err != nil {
		// The options are fixed; only a programming error makes them invalid.
		panic(err)
	}
	return &Vault{dir: vaultDir, kv: kv}
}

// options are the kit vault options of the vault directory dir.
func options(dir string) kitvault.Options {
	return kitvault.Options{
		Dir: dir,
		Files: kitvault.Files{
			Key: keyFile, Recipient: recipientFile, Document: secretsFile, Pending: pendingDir,
			Plain: plainFile, Sealed: []string{tokensFile, settingsFile},
		},
		Schemas:       []int{schemaVersion},
		EmptyDocument: emptyDocument,
		SwitchFilter:  withoutApprovals,
		Merge:         mergeFunc(dir),
		WorkFactor:    scryptLogN,
	}
}

// Dir returns the vault directory.
func (v *Vault) Dir() string { return v.dir }

func (v *Vault) secretsPath() string { return filepath.Join(v.dir, secretsFile) }
func (v *Vault) plainPath() string   { return filepath.Join(v.dir, plainFile) }

// State reports what the vault currently is. It reads no secret.
func (v *Vault) State() (State, error) { return v.kv.State() }

// Forget drops the identity and document this process holds for an encrypted vault it unlocked, so a later
// read asks for the passphrase again. It touches no file: a vault process outside this one is locked
// separately (see vaultproc.LockProcess).
func (v *Vault) Forget() { v.kv.Forget() }

// Checkpoint records the files a write of secrets or approvals changes, the document, encrypted or not, the
// key, the recipient and the pending entries, together with what this process holds of the vault, and
// returns restore, which puts them back byte for byte without a passphrase and removes every pending entry
// added since. Agent tokens and settings are not covered.
func (v *Vault) Checkpoint() (restore func() error, err error) { return v.kv.Checkpoint() }

// load returns the document of an unencrypted vault or one unlocked in this process. Approvals in a
// plaintext document are never trusted.
func (v *Vault) load() (*document, error) {
	state, err := v.kv.State()
	if err != nil {
		return nil, err
	}
	data, err := v.kv.Document()
	if err != nil {
		return nil, mapDocumentErr(err)
	}
	defer clear(data)
	path := v.secretsPath()
	if state == StateUnencrypted {
		path = v.plainPath()
	}
	doc, err := decodeDocument(path, data)
	if err != nil {
		return nil, err
	}
	if state != StateUnlocked {
		doc.Approvals = nil
	}
	return doc, nil
}

// encode encodes doc for the vault's current mode: an encrypted document re-labels approvals granted under
// the legacy fingerprint, a plain one never holds approvals.
func (v *Vault) encode(doc *document) ([]byte, error) {
	state, err := v.kv.State()
	if err != nil {
		return nil, err
	}
	if state == StateUnlocked {
		doc.upgradeFingerprints()
		return encodeDocument(v.secretsPath(), doc)
	}
	plain := *doc
	plain.Approvals = nil
	return encodeDocument(v.plainPath(), &plain)
}

// write replaces the document of an unencrypted vault or one unlocked in this process.
func (v *Vault) write(doc *document) error {
	data, err := v.encode(doc)
	if err != nil {
		return err
	}
	defer clear(data)
	return mapDocumentErr(v.kv.Write(data))
}

// writeWithUndo is write that also returns the kit's undo, which puts the previous document file back byte
// for byte and touches no other file.
func (v *Vault) writeWithUndo(doc *document) (func() error, error) {
	data, err := v.encode(doc)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	undo, err := v.kv.WriteWithUndo(data)
	if err != nil {
		return nil, mapDocumentErr(err)
	}
	return undo, nil
}

// identity returns the private key of the vault unlocked in this process without reading its document. A
// locked vault fails with ErrNotUnlocked, any other with ErrNotEncrypted.
func (v *Vault) identity() (*age.X25519Identity, error) {
	state, err := v.kv.State()
	if err != nil {
		return nil, err
	}
	switch state {
	case StateUnlocked:
		return v.kv.Identity()
	case StateLocked:
		return nil, ErrNotUnlocked
	}
	return nil, ErrNotEncrypted
}

// LogKey returns the invocation log key of a vault unlocked in this process, derived from its key (see
// invokelog.DeriveKey). It fails with ErrNotUnlocked unless the vault was unlocked, or created encrypted, in
// this process. The key is the caller's to clear.
func (v *Vault) LogKey() (*invokelog.Key, error) {
	key, err := v.kv.Identity()
	if err != nil {
		return nil, ErrNotUnlocked
	}
	return invokelog.DeriveKey(key)
}

// Recipient returns the public key an encrypted vault encrypts to, as age text. It needs no passphrase and
// fails with ErrNotEncrypted for a vault without one.
func (v *Vault) Recipient() (string, error) {
	recipient, err := v.kv.Recipient()
	if errors.Is(err, kitvault.ErrAbsent) || isNotExist(err) {
		return "", ErrNotEncrypted
	}
	return recipient, err
}

// OpenWithKey returns the encrypted vault in dir, the vault directory itself as Dir reports it, unlocked with
// key, its own private key, the way a vault process holds it, without the passphrase. It reads secrets.age as
// it is on disk now and leaves every pending entry where it is: the next unlock with the passphrase merges
// them, and a write through the returned vault keeps them pending.
func OpenWithKey(dir string, key *age.X25519Identity) (*Vault, error) {
	if key == nil {
		return nil, ErrNotUnlocked
	}
	kv, err := kitvault.OpenWithIdentity(options(dir), key)
	if err != nil {
		if errors.Is(err, kitvault.ErrAbsent) {
			return nil, ErrNotEncrypted
		}
		return nil, mapDocumentErr(err)
	}
	return &Vault{dir: dir, kv: kv}, nil
}

// mapDocumentErr gives a kit document error the message this package always used; it still unwraps to the
// kit error. Any other error is returned as it is.
func mapDocumentErr(err error) error {
	var de *kitvault.DocumentError
	if !errors.As(err, &de) {
		return err
	}
	switch de.Kind {
	case kitvault.DocumentDecode:
		return &documentError{msg: fmt.Sprintf("cannot read %s: not a valid vault document", de.Path), err: err}
	case kitvault.DocumentSchema:
		return &documentError{msg: fmt.Sprintf("cannot read %s: unsupported vault schema version %d, want %d",
			de.Path, de.Version, schemaVersion), err: err}
	}
	return err
}

// documentError is a damaged or unknown vault document: its message and the kit error it stands for.
type documentError struct {
	msg string
	err error
}

func (e *documentError) Error() string { return e.msg }
func (e *documentError) Unwrap() error { return e.err }

func isNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }
