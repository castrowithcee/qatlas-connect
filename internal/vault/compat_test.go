package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// The vaults in testdata/compat/<release> were written by that release (see generate.sh there): a plain
// vault, and an encrypted one with entries, two approvals, settings, an agent token and three pending writes
// another process queued while it found the vault locked. v2.2.0 still wrote under the labels from before the
// rename, v3.3.0 under the current ones.
var compatReleases = []string{"v2.2.0", "v3.3.0"}

type compatMeta struct {
	Passphrase string  `json:"passphrase"`
	Scopes     []Scope `json:"scopes"`
}

// compatSettings are the settings both releases wrote.
var compatSettings = Settings{Update: UpdateLock, IdleTimeout: 7 * time.Hour, AdminTimeout: 3 * time.Minute,
	RetentionDays: 33, MaxUnlock: 20 * time.Hour}

// copyCompat copies the vault of mode written by release into a fresh home, privately as a vault requires,
// with or without its pending writes, and returns it with the fixture's meta data.
func copyCompat(t *testing.T, release, mode string, pending bool) (*Vault, compatMeta) {
	t.Helper()
	lowWorkFactor(t)
	root := filepath.Join("testdata", "compat", release)
	var meta compatMeta
	raw, err := os.ReadFile(filepath.Join(root, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	v := New(t.TempDir())
	copyPrivate(t, filepath.Join(root, mode), v.Dir())
	if pending {
		copyPrivate(t, filepath.Join(root, mode, pendingDir), filepath.Join(v.Dir(), pendingDir))
	}
	return v, meta
}

// copyPrivate copies the regular files of src into dst, the directory 0700 and every file 0600.
func copyPrivate(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func mustValue(t *testing.T, v *Vault, name, role, want string) {
	t.Helper()
	got, found, _, err := v.Get(name, role, nil)
	if err != nil || !found || got != want {
		t.Fatalf("Get(%s, %s) = found %v, err %v, want the value of the fixture", name, role, found, err)
	}
}

// A vault of either release unlocks and reads as it is: the secrets, the credential ids, both approvals,
// the settings and the agent token, and a read writes nothing.
func TestCompatEncryptedReadsAsItIs(t *testing.T) {
	for _, release := range compatReleases {
		t.Run(release, func(t *testing.T) {
			v, meta := copyCompat(t, release, "encrypted", false)
			before := filesOf(t, v.Dir())
			if merged, err := v.Unlock(meta.Passphrase); err != nil || merged != 0 {
				t.Fatalf("Unlock() = %d, %v", merged, err)
			}
			mustValue(t, v, "gh-a", "token", "v-a")
			mustValue(t, v, "fw-b", "user", "u-b")
			ids, err := v.EntryCredentialIDs()
			if err != nil || ids["gh-a"] != "cid-a" || ids["fw-b"] != "" {
				t.Fatalf("EntryCredentialIDs() = %v, %v", ids, err)
			}

			scopeA, scopeF := meta.Scopes[0], meta.Scopes[1]
			if err := v.CheckApproval(scopeA); err != nil {
				t.Errorf("CheckApproval(conn-a) = %v, want the approval to hold", err)
			}
			if err := v.CheckApprovalFor(scopeF, "fw-b"); err != nil {
				t.Errorf("CheckApprovalFor(conn-f, fw-b) = %v, want the approval to hold", err)
			}
			bindings, err := v.Bindings()
			if err != nil || !bindings.AllowsRole(scopeF, "fw-b", "user") || bindings.AllowsRole(scopeF, "fw-b", "key") {
				t.Errorf("Bindings() releases the wrong roles of the forward credential, err %v", err)
			}
			approvals, err := v.Approvals()
			if err != nil || approvals["conn-a"].ConnectionID != "cid-conn-a" {
				t.Fatalf("Approvals() = %v, %v", approvals, err)
			}
			current := approvals["conn-a"].Fingerprint == Fingerprint(scopeA, approvals["conn-a"].CredentialID)
			if current != (release != "v2.2.0") {
				t.Errorf("approval under the current label = %v, want it only from the current release", current)
			}

			if settings, err := v.Settings(); err != nil || settings != compatSettings {
				t.Errorf("Settings() = %+v, %v", settings, err)
			}
			tokens, err := v.Tokens()
			if err != nil || len(tokens) != 1 || tokens[0].Name != "agent-a" || !slices.Equal(tokens[0].Models, []string{"conn-a"}) {
				t.Fatalf("Tokens() = %+v, %v", tokens, err)
			}
			result, err := v.ApproveWithToken(tokens[0].Value, meta.Scopes, nil, time.Now())
			if err != nil || len(result.Changes) != 0 {
				t.Errorf("ApproveWithToken() = %+v, %v; an approved connection must not read as open", result, err)
			}

			after := filesOf(t, v.Dir())
			// ApproveWithToken decides under the write lock, whose empty file stays.
			delete(after, writeLockFile)
			if !sameFiles(before, after) {
				t.Error("reading the vault changed its files")
			}
		})
	}
}

// Unlocking merges the pending writes of either release, and the write that merges them re-labels a legacy
// approval, which then holds in a fresh process too. A changed scope or a forged fingerprint stays unapproved.
func TestCompatPendingMergeAndRelabel(t *testing.T) {
	for _, release := range compatReleases {
		t.Run(release, func(t *testing.T) {
			v, meta := copyCompat(t, release, "encrypted", true)
			if merged, err := v.Unlock(meta.Passphrase); err != nil || merged != 3 {
				t.Fatalf("Unlock() = %d, %v, want the 3 pending writes merged", merged, err)
			}
			mustValue(t, v, "gh-a", "token", "v-a2")
			mustValue(t, v, "new-c", "token", "v-c")
			mustValue(t, v, "fw-b", "key", "k-b")
			if ids, _ := v.EntryCredentialIDs(); ids["fw-b"] != "cid-b" || ids["gh-a"] != "cid-a" {
				t.Errorf("EntryCredentialIDs() = %v", ids)
			}
			if names, _ := v.pendingFiles(); len(names) != 0 {
				t.Errorf("pending after Unlock() = %v, want none", names)
			}

			scopeA := meta.Scopes[0]
			approvals, _ := v.Approvals()
			if approvals["conn-a"].Fingerprint != Fingerprint(scopeA, approvals["conn-a"].CredentialID) {
				t.Error("the merging write did not re-label the approval")
			}
			again := New(filepath.Dir(v.Dir()))
			if _, err := again.Unlock(meta.Passphrase); err != nil {
				t.Fatal(err)
			}
			for _, scope := range meta.Scopes {
				if err := again.CheckApproval(scope); err != nil {
					t.Errorf("CheckApproval(%s) in a fresh process = %v", scope.Connection, err)
				}
			}
			changed := scopeA
			changed.Targets = []string{"o/other"}
			if err := again.CheckApproval(changed); !errors.Is(err, ErrApprovalRequired) {
				t.Errorf("CheckApproval() of a changed scope = %v, want ErrApprovalRequired", err)
			}
			if s, err := again.Settings(); err != nil || s != compatSettings {
				t.Errorf("Settings() = %+v, %v", s, err)
			}
		})
	}
}

// An approval whose fingerprint was made under a foreign label, or altered, is refused, and no write
// re-labels it into one that holds.
func TestCompatForgedApprovalStaysRefused(t *testing.T) {
	for _, release := range compatReleases {
		t.Run(release, func(t *testing.T) {
			v, meta := copyCompat(t, release, "encrypted", false)
			if _, err := v.Unlock(meta.Passphrase); err != nil {
				t.Fatal(err)
			}
			scopeA := meta.Scopes[0]
			for name, forge := range map[string]func(Approval) string{
				"foreign label": func(a Approval) string {
					return fingerprintWith("qatlas-connect agent tokens v1\x00", scopeA, a.CredentialID)
				},
				"altered": func(a Approval) string {
					b := []byte(a.Fingerprint)
					b[0] ^= 1
					return string(b)
				},
			} {
				doc := heldDocument(t, v)
				approval := doc.Approvals["conn-a"]
				approval.Fingerprint = forge(approval)
				doc.Approvals["conn-a"] = approval
				writeSecretsDoc(t, v, doc)
				if err := v.CheckApproval(scopeA); !errors.Is(err, ErrApprovalRequired) {
					t.Errorf("%s: CheckApproval() = %v, want ErrApprovalRequired", name, err)
				}
				if err := v.Set("other", "token", "x", nil); err != nil {
					t.Fatal(err)
				}
				if err := v.CheckApproval(scopeA); !errors.Is(err, ErrApprovalRequired) {
					t.Errorf("%s: CheckApproval() after a write = %v, want ErrApprovalRequired", name, err)
				}
			}
		})
	}
}

// A rename in a vault unlocked while another process queued writes: the writes count per name, a rename onto
// a name that has a write waiting is refused, only the writes of the old name are merged, and the undo puts
// the document and exactly those writes back byte for byte while a write queued since stays.
func TestCompatRenameWithPendingWrites(t *testing.T) {
	for _, release := range compatReleases {
		t.Run(release, func(t *testing.T) {
			v, meta := copyCompat(t, release, "encrypted", false)
			if _, err := v.Unlock(meta.Passphrase); err != nil {
				t.Fatal(err)
			}
			// The pending writes arrive after this process unlocked.
			copyPrivate(t, filepath.Join("testdata", "compat", release, "encrypted", pendingDir), v.pendingPath())
			before := filesOf(t, v.Dir())
			pendingBefore := filesOf(t, v.pendingPath())

			counts, err := v.PendingWrites()
			if err != nil || len(counts) != 3 || counts["gh-a"] != 1 || counts["new-c"] != 1 || counts["fw-b"] != 1 {
				t.Fatalf("PendingWrites() = %v, %v", counts, err)
			}
			if _, _, err := v.RenameCredential(CredentialRename{From: "fw-b", To: "new-c", Entry: true}); !errors.Is(err, ErrEntryTaken) {
				t.Fatalf("RenameCredential() onto a waiting name = %v, want ErrEntryTaken", err)
			}
			if !sameFiles(before, filesOf(t, v.Dir())) || !sameFiles(pendingBefore, filesOf(t, v.pendingPath())) {
				t.Fatal("a refused rename changed files")
			}

			scopeA := meta.Scopes[0]
			after := scopeA
			after.Credential = "gh-z"
			carried, undo, err := v.RenameCredential(CredentialRename{From: "gh-a", To: "gh-z", Entry: true,
				Carry: []ScopeRename{{Before: scopeA, After: after, ConnectionID: "cid-conn-a"}}})
			if err != nil || !slices.Equal(carried, []string{"conn-a"}) {
				t.Fatalf("RenameCredential() = %v, %v", carried, err)
			}
			mustValue(t, v, "gh-z", "token", "v-a2")
			if err := v.CheckApproval(after); err != nil {
				t.Errorf("CheckApproval() after the rename = %v", err)
			}
			left := filesOf(t, v.pendingPath())
			if len(left) != 2 {
				t.Fatalf("pending after the rename = %d, want the writes of the other names", len(left))
			}
			for name, data := range left {
				if !bytes.Equal(pendingBefore[name], data) {
					t.Errorf("pending %s changed", name)
				}
			}

			// Another process queues a write between the rename and its undo.
			other := New(filepath.Dir(v.Dir()))
			if err := other.Set("late", "token", "x", nil); err != nil {
				t.Fatal(err)
			}
			if err := undo(); err != nil {
				t.Fatalf("undo() = %v", err)
			}
			now := filesOf(t, v.Dir())
			if !bytes.Equal(now[secretsFile], before[secretsFile]) {
				t.Error("undo did not restore secrets.age byte for byte")
			}
			pendingNow := filesOf(t, v.pendingPath())
			for name, data := range pendingBefore {
				if !bytes.Equal(pendingNow[name], data) {
					t.Errorf("undo did not restore pending %s byte for byte", name)
				}
			}
			if len(pendingNow) != len(pendingBefore)+1 {
				t.Errorf("pending after undo = %d, want the restored writes and the late one", len(pendingNow))
			}
			mustValue(t, v, "gh-a", "token", "v-a")
		})
	}
}

// The plain vault of either release reads as it is and still turns into an encrypted one.
func TestCompatPlainVault(t *testing.T) {
	for _, release := range compatReleases {
		t.Run(release, func(t *testing.T) {
			v, _ := copyCompat(t, release, "plain", false)
			value, found, state, err := v.Get("gh-p", "token", nil)
			if err != nil || !found || value != "v-plain" || state != StateUnencrypted {
				t.Fatalf("Get() = found %v, state %v, err %v", found, state, err)
			}
			if err := v.Encrypt("new-phrase"); err != nil {
				t.Fatalf("Encrypt() = %v", err)
			}
			mustValue(t, v, "gh-p", "user", "u-plain")
		})
	}
}

// A damaged secrets document leaves the stored settings readable with the key alone.
func TestCompatDamagedDocumentKeepsSettings(t *testing.T) {
	for _, release := range compatReleases {
		t.Run(release, func(t *testing.T) {
			v, meta := copyCompat(t, release, "encrypted", false)
			if _, err := v.Unlock(meta.Passphrase); err != nil {
				t.Fatal(err)
			}
			key := testKey(t, v)
			path := v.secretsPath()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)-5] ^= 0xff
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = New(filepath.Dir(v.Dir())).Unlock(meta.Passphrase)
			if !errors.Is(err, kitvault.ErrDamaged) || !strings.HasPrefix(err.Error(), "cannot decrypt "+path) {
				t.Errorf("Unlock() of a damaged document = %v, want a damaged-file error", err)
			}
			if _, err := OpenWithKey(v.Dir(), key); !errors.Is(err, kitvault.ErrDamaged) {
				t.Errorf("OpenWithKey() of a damaged document = %v, want a damaged-file error", err)
			}
			if s, err := ReadSettings(v.Dir(), key); err != nil || s != compatSettings {
				t.Errorf("ReadSettings() = %+v, %v, want the stored settings", s, err)
			}
		})
	}
}

// An empty secrets.json is an empty document; a damaged or unknown one keeps its message and tells its kind.
func TestPlainDocumentErrors(t *testing.T) {
	v := New(t.TempDir())
	if err := os.MkdirAll(v.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	path := v.plainPath()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("")
	if _, found, state, err := v.Get("x", "token", nil); err != nil || found || state != StateUnencrypted {
		t.Fatalf("Get() of an empty secrets.json = found %v, state %v, err %v", found, state, err)
	}
	if st, err := v.Status(); err != nil || st.Entries != 0 {
		t.Fatalf("Status() of an empty secrets.json = %+v, %v", st, err)
	}
	if err := v.Set("x", "token", "y", nil); err != nil {
		t.Fatalf("Set() on an empty secrets.json = %v", err)
	}
	mustValue(t, v, "x", "token", "y")

	write("not json")
	_, _, _, err := v.Get("x", "token", nil)
	if err == nil || err.Error() != "cannot read "+path+": not a valid vault document" || !errors.Is(err, kitvault.ErrDamaged) {
		t.Errorf("Get() of a damaged secrets.json = %v", err)
	}
	write(`{"schema":2}`)
	_, _, _, err = v.Get("x", "token", nil)
	if err == nil || err.Error() != "cannot read "+path+": unsupported vault schema version 2, want 1" ||
		!errors.Is(err, kitvault.ErrUnsupportedSchema) {
		t.Errorf("Get() of an unknown schema = %v", err)
	}
	write(`{"schema":1,"entries":[]}`)
	_, _, _, err = v.Get("x", "token", nil)
	if err == nil || err.Error() != "cannot read "+path+": not a valid vault document" || !errors.Is(err, kitvault.ErrDamaged) {
		t.Errorf("Get() of a malformed document = %v", err)
	}
}

func sameFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, data := range a {
		if other, ok := b[name]; !ok || !bytes.Equal(data, other) {
			return false
		}
	}
	return true
}
