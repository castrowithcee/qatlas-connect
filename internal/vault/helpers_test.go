package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// testKey returns the private key of the vault unlocked in this process.
func testKey(t *testing.T, v *Vault) *age.X25519Identity {
	t.Helper()
	key, err := v.kv.Identity()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// readSealed returns the plaintext of the sealed file name of the vault unlocked in this process.
func readSealed(t *testing.T, v *Vault, name string) []byte {
	t.Helper()
	plain, err := v.kv.ReadSealed(name)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

// writeSealed replaces the sealed file name of the vault unlocked in this process with plaintext.
func writeSealed(t *testing.T, v *Vault, name string, plain []byte) {
	t.Helper()
	if err := v.kv.WriteSealed(name, plain); err != nil {
		t.Fatal(err)
	}
}

// writeSecretsDoc replaces the document of the vault unlocked in this process with doc as it is, without
// re-labelling any approval.
func writeSecretsDoc(t *testing.T, v *Vault, doc *document) {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.kv.Write(data); err != nil {
		t.Fatal(err)
	}
}

// heldDocument returns the document of an unencrypted vault or one unlocked in this process.
func heldDocument(t *testing.T, v *Vault) *document {
	t.Helper()
	doc, err := v.load()
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// plainDocument reads secrets.json of the vault directly, approvals included.
func plainDocument(t *testing.T, v *Vault) *document {
	t.Helper()
	data, err := os.ReadFile(v.plainPath())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := decodeDocument(v.plainPath(), data)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func settingsMACWith(key *age.X25519Identity, doc settingsDocument, label string) (string, error) {
	input, ok := settingsMACInput(doc)
	if !ok {
		return "", errors.New("unknown settings schema")
	}
	return kitvault.MAC(key, label, input)
}

func tokenMACWith(key *age.X25519Identity, tokens []Token, label string) (string, error) {
	data, err := json.Marshal(tokens)
	if err != nil {
		return "", err
	}
	return kitvault.MAC(key, label, data)
}

func (v *Vault) keyPath() string     { return filepath.Join(v.dir, keyFile) }
func (v *Vault) pendingPath() string { return filepath.Join(v.dir, pendingDir) }
func (v *Vault) pendingFiles() ([]string, error) {
	entries, err := os.ReadDir(v.pendingPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".age" && e.Name()[0] != '.' {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// loadPlainDocument reads secrets.json the way an unencrypted vault does: approvals are dropped.
func loadPlainDocument(path string) (*document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := decodeDocument(path, data)
	if err != nil {
		return nil, err
	}
	doc.Approvals = nil
	return doc, nil
}

// encryptToRecipient encrypts plaintext to the public recipient alone, as anyone who can write the vault
// directory can.
func encryptToRecipient(plaintext string, recipientText []byte) ([]byte, error) {
	recipient, err := age.ParseX25519Recipient(strings.TrimSpace(string(recipientText)))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, plaintext); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func settingsMAC(key *age.X25519Identity, doc settingsDocument) (string, error) {
	return settingsMACWith(key, doc, settingsMACLabel)
}
