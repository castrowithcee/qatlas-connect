package vault

// This file is not built with the module: generate.sh copies it into the code of a release and runs it
// there, so the vault files beside it are exactly what that release writes. The secrets are short canaries.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/cryptotest"
	"testing/synctest"
	"time"
)

const fixturePhrase = "fixture-phrase"

func fixtureScopes() []Scope {
	return []Scope{
		{Connection: "conn-a", Credential: "gh-a", Provider: "prov-a", Origin: "https://a.example",
			Effects: []string{"read"}, Targets: []string{"o/a"}, Tools: []string{"t.list"}},
		{Connection: "conn-f", Credential: "gh-a", Provider: "prov-a", Origin: "https://a.example",
			Effects: []string{"write", "read"}, Targets: []string{"o/b", "o/a"}, Exclude: []string{"o/a/x"},
			Paths: []string{"/w/p"}, FilesRead: []string{"/w/in"}, FilesWrite: []string{"/w/out"},
			Forward:     []ForwardSecret{{Name: "fw-b", Fields: []string{"user"}}},
			ActiveUntil: "2099-01-01T00:00:00Z"},
	}
}

func TestGenerateFixture(t *testing.T) {
	out := os.Getenv("FIXTURE_OUT")
	if out == "" {
		t.Skip("FIXTURE_OUT is not set")
	}
	LowerWorkFactorForTests()
	cryptotest.SetGlobalRandom(t, 1231)
	// The bubble's clock stands still, so every recorded time is the same on every run.
	synctest.Test(t, func(t *testing.T) { generateFixture(t, out) })
}

func generateFixture(t *testing.T, out string) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	offer := func(string) (string, error) { return fixturePhrase, nil }

	plain := New(filepath.Join(home, "plain"))
	must(plain.Set("gh-p", "token", "v-plain", nil))
	must(plain.Set("gh-p", "user", "u-plain", nil))
	_, _, err := plain.RecordCredentialIDs(map[string]string{"gh-p": "cid-p"})
	must(err)

	enc := New(filepath.Join(home, "enc"))
	must(enc.Set("gh-a", "token", "v-a", offer))
	must(enc.Set("fw-b", "user", "u-b", nil))
	must(enc.Set("fw-b", "key", "k-b", nil))
	_, _, err = enc.RecordCredentialIDs(map[string]string{"gh-a": "cid-a"})
	must(err)
	must(enc.ApproveConnections(fixtureScopes(), map[string]string{"conn-a": "cid-conn-a"}))
	must(enc.SetSettings(Settings{Update: UpdateLock, IdleTimeout: 7 * time.Hour, AdminTimeout: 3 * time.Minute,
		RetentionDays: 33, MaxUnlock: 20 * time.Hour}))
	_, err = enc.CreateToken("agent-a", []string{"conn-a"}, nil)
	must(err)

	// Another process finds the vault locked and queues its writes.
	locked := New(filepath.Join(home, "enc"))
	must(locked.Set("gh-a", "token", "v-a2", nil))
	must(locked.Set("new-c", "token", "v-c", nil))
	_, _, err = locked.RecordCredentialIDs(map[string]string{"fw-b": "cid-b"})
	must(err)

	copyDir(t, plain.Dir(), filepath.Join(out, "plain"))
	copyDir(t, enc.Dir(), filepath.Join(out, "encrypted"))
	copyDir(t, filepath.Join(enc.Dir(), "pending"), filepath.Join(out, "encrypted", "pending"))

	meta, err := json.MarshalIndent(map[string]any{"passphrase": fixturePhrase, "scopes": fixtureScopes()}, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(out, "meta.json"), append(meta, '\n'), 0o644))
}

// copyDir copies the regular files of src into dst.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
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
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
