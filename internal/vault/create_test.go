package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

const createPassphrase = "synthetic-passphrase-create"

// The vault's first Set writes the vault with its entry at once; its undo removes the entry and leaves the
// vault, as it always did. It needs no passphrase, also once this process forgot the key, and leaves what
// another writer stored since.
func TestFirstSetAndItsUndo(t *testing.T) {
	lowWorkFactor(t)
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%v", encrypted), func(t *testing.T) {
			v := New(t.TempDir())
			offer, want := PassphraseFunc(nil), StateUnencrypted
			if encrypted {
				// The undo runs after Forget.
				offer, want = offering(createPassphrase), StateLocked
			}
			undo, err := v.SetUndoable("wiki-reader", "token", "synthetic-first", offer)
			if err != nil {
				t.Fatal(err)
			}
			if got, found, _, err := New(filepath.Dir(v.Dir())).Get("wiki-reader", "token",
				offering(createPassphrase)); err != nil || !found || got != "synthetic-first" {
				t.Fatalf("Get() after the first Set = %q, %v, %v", got, found, err)
			}
			other := New(filepath.Dir(v.Dir()))
			if _, _, _, err := other.Get("wiki-reader", "token", offering(createPassphrase)); err != nil {
				t.Fatal(err)
			}
			if err := other.Set("wiki-reader", "other", "synthetic-other", nil); err != nil {
				t.Fatal(err)
			}
			v.Forget()
			if err := undo(); err != nil {
				t.Fatalf("undo = %v", err)
			}
			if state, err := v.State(); err != nil || state != want {
				t.Errorf("State() after the undo = %q, %v; want %q", state, err, want)
			}
			fresh := New(filepath.Dir(v.Dir()))
			if _, found, _, err := fresh.Get("wiki-reader", "token", offering(createPassphrase)); err != nil || found {
				t.Errorf("Get(token) after the undo = found %v, %v; want the role gone", found, err)
			}
			if got, _, _, err := fresh.Get("wiki-reader", "other", nil); err != nil || got != "synthetic-other" {
				t.Errorf("Get(other) after the undo = %q, %v; want the other writer's value", got, err)
			}
		})
	}
}

// A first Set that cannot write the document leaves no vault behind, not even an empty one.
func TestFirstSetThatFailsLeavesNoVault(t *testing.T) {
	lowWorkFactor(t)
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%v", encrypted), func(t *testing.T) {
			v := New(t.TempDir())
			offer, document := PassphraseFunc(nil), v.plainPath()
			if encrypted {
				offer, document = offering(createPassphrase), v.secretsPath()
			}
			// A directory where the document belongs makes its write fail.
			if err := os.MkdirAll(document, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := v.Set("wiki-reader", "token", "synthetic-first", offer); err == nil {
				t.Fatal("Set() succeeded although the document could not be written")
			}
			if err := os.Remove(document); err != nil {
				t.Fatal(err)
			}
			if state, err := New(filepath.Dir(v.Dir())).State(); err != nil || state != StateAbsent {
				t.Errorf("State() after the failed first Set = %q, %v; want absent", state, err)
			}
		})
	}
}

// A write of a vault value that another process switched to unencrypted since keeps the message it always
// had and still reports the kit's error.
func TestWriteOfASwitchedVault(t *testing.T) {
	e := newConcEnv(t, true)
	if err := New(e.dir).Decrypt(concPassphrase); err != nil {
		t.Fatal(err)
	}
	err := e.a.Set(rotCredential, "client_id", "synthetic-client", nil)
	want := "vault: another process switched the vault between encrypted and unencrypted; try again"
	if err == nil || err.Error() != want || !errors.Is(err, kitvault.ErrSwitched) {
		t.Fatalf("Set() = %v, want %q wrapping the kit's ErrSwitched", err, want)
	}
}

// A revert that meets a damaged document names the document of the vault's mode.
func TestRevertNamesTheDocumentOfTheMode(t *testing.T) {
	for _, plain := range []bool{false, true} {
		dir := t.TempDir()
		want := secretsFile
		if plain {
			want = plainFile
			if err := os.WriteFile(filepath.Join(dir, plainFile), emptyDocument, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, err := revertFunc(dir)(nil, emptyDocument, []byte("[]"))
		if err == nil || !strings.Contains(err.Error(), want+": not a valid vault document") {
			t.Errorf("plain=%v: revert = %v, want it to name %s", plain, err, want)
		}
	}
}
