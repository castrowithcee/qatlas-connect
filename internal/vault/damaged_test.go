package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

const damagedPassphrase = "synthetic-passphrase-damaged"

// damagedVault returns the directory of an encrypted vault holding one entry, locked in no process.
func damagedVault(t *testing.T) (dir string, v *Vault) {
	t.Helper()
	lowWorkFactor(t)
	dir = t.TempDir()
	if err := New(dir).Set("wiki-reader", "token", "synthetic-token", offering(damagedPassphrase)); err != nil {
		t.Fatal(err)
	}
	return dir, New(dir)
}

// A secrets.age that does not decrypt is reported with this package's own text for the cause, never with a
// library message, and still as a damaged file.
func TestUndecryptableDocumentNamesItsCause(t *testing.T) {
	cases := []struct {
		name, want string
		replace    func(t *testing.T, data []byte) []byte
	}{
		{"damaged", "the file is damaged or was altered", func(t *testing.T, data []byte) []byte {
			data[len(data)-5] ^= 0xff
			return data
		}},
		{"not age", "the file is not an encrypted vault file", func(*testing.T, []byte) []byte {
			return []byte(`{"schema":1}`)
		}},
		{"wrong key", "the file is encrypted to another key than this vault's", func(t *testing.T, _ []byte) []byte {
			other, err := age.GenerateX25519Identity()
			if err != nil {
				t.Fatal(err)
			}
			data, err := encryptToRecipient(`{"schema":1}`, []byte(other.Recipient().String()))
			if err != nil {
				t.Fatal(err)
			}
			return data
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, v := damagedVault(t)
			data, err := os.ReadFile(v.secretsPath())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(v.secretsPath(), c.replace(t, data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = v.Unlock(damagedPassphrase)
			want := "cannot decrypt " + v.secretsPath() + ": " + c.want
			if err == nil || err.Error() != want || !errors.Is(err, kitvault.ErrDamaged) {
				t.Errorf("Unlock() = %v, want %q", err, want)
			}
		})
	}
}

// A document without a schema, or with a null one, reports schema version 0 as it always did.
func TestDocumentWithoutSchemaReportsVersionZero(t *testing.T) {
	for _, content := range []string{`{"entries":{}}`, `{"schema":null}`} {
		v := New(t.TempDir())
		if err := os.MkdirAll(v.Dir(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(v.plainPath(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := v.Get("wiki-reader", "token", nil)
		want := "cannot read " + v.plainPath() + ": unsupported vault schema version 0, want 1"
		if err == nil || err.Error() != want || !errors.Is(err, kitvault.ErrUnsupportedSchema) {
			t.Errorf("Get() of %s = %v, want %q", content, err, want)
		}
	}
}

// Decrypt turns an encrypted vault whose secrets.age is missing into an empty unencrypted one, as it always
// did, and merges what is pending.
func TestDecryptWithoutTheDocument(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%v", pending), func(t *testing.T) {
			_, v := damagedVault(t)
			if pending {
				if err := New(filepath.Dir(v.Dir())).Set("wiki-reader", "user", "synthetic-user", nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(v.secretsPath()); err != nil {
				t.Fatal(err)
			}
			if err := v.Decrypt("synthetic-wrong-passphrase"); !errors.Is(err, ErrWrongPassphrase) {
				t.Fatalf("Decrypt() with a wrong passphrase = %v", err)
			}
			if _, err := os.Stat(v.secretsPath()); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a wrong passphrase wrote secrets.age: %v", err)
			}
			if err := v.Decrypt(damagedPassphrase); err != nil {
				t.Fatalf("Decrypt() = %v", err)
			}
			if state, err := v.State(); err != nil || state != StateUnencrypted {
				t.Fatalf("State() = %q, %v; want unencrypted", state, err)
			}
			if _, found := mustGet(t, v, "wiki-reader", "token"); found {
				t.Error("the secret of the missing document came back")
			}
			got, found := mustGet(t, v, "wiki-reader", "user")
			if found != pending || pending && got != "synthetic-user" {
				t.Errorf("the pending entry = %q, %v; want it merged only where it was pending", got, found)
			}
		})
	}
}

// Without the recipient file the settings are still read with the vault key, and their check value decides
// whether they are trusted.
func TestReadSettingsWithoutTheRecipient(t *testing.T) {
	_, v := damagedVault(t)
	if _, err := v.Unlock(damagedPassphrase); err != nil {
		t.Fatal(err)
	}
	key := testKey(t, v)
	if err := os.Remove(filepath.Join(v.Dir(), recipientFile)); err != nil {
		t.Fatal(err)
	}
	if s, err := ReadSettings(v.Dir(), key); err != nil || s != DefaultSettings() {
		t.Errorf("ReadSettings() without settings.age = %+v, %v; want the defaults", s, err)
	}
	stored := DefaultSettings()
	stored.Update = UpdateLock
	recipient := []byte(key.Recipient().String() + "\n")
	if err := os.WriteFile(filepath.Join(v.Dir(), recipientFile), recipient, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.SetSettings(stored); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(v.Dir(), recipientFile)); err != nil {
		t.Fatal(err)
	}
	if s, err := ReadSettings(v.Dir(), key); err != nil || s != stored {
		t.Errorf("ReadSettings() = %+v, %v; want the stored settings", s, err)
	}
}
