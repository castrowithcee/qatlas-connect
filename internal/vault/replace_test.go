package vault

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

const (
	replaceCredential = "consent-login"
	replaceRole       = "refresh_token"
	replacePassphrase = "synthetic-passphrase-replace"
)

func offerReplacePassphrase(string) (string, error) { return replacePassphrase, nil }

// A replacement happens only while the role still holds the old value; otherwise it reports what is stored
// and writes nothing, and it never creates a role.
func TestReplaceRoleComparesWithTheOldValue(t *testing.T) {
	lowWorkFactor(t)
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%v", encrypted), func(t *testing.T) {
			dir := t.TempDir()
			v := New(dir)
			offer := PassphraseFunc(nil)
			if encrypted {
				offer = offerReplacePassphrase
			}
			if err := v.Set(replaceCredential, replaceRole, "synthetic-refresh-old", offer); err != nil {
				t.Fatal(err)
			}

			current, replaced, err := v.ReplaceRole(replaceCredential, replaceRole, "synthetic-refresh-other", "synthetic-refresh-new")
			if err != nil || replaced || current != "synthetic-refresh-old" {
				t.Fatalf("ReplaceRole() with a stale old value = %q, %v, %v; want the stored value and no write", current, replaced, err)
			}
			current, replaced, err = v.ReplaceRole(replaceCredential, replaceRole, "synthetic-refresh-old", "synthetic-refresh-new")
			if err != nil || !replaced || current != "synthetic-refresh-new" {
				t.Fatalf("ReplaceRole() = %q, %v, %v; want the replacement", current, replaced, err)
			}
			current, replaced, err = v.ReplaceRole(replaceCredential, "other_role", "synthetic-refresh-old", "synthetic-refresh-new")
			if err != nil || replaced || current != "" {
				t.Fatalf("ReplaceRole() of a missing role = %q, %v, %v; want nothing created", current, replaced, err)
			}

			reread := New(dir)
			ask := PassphraseFunc(nil)
			if encrypted {
				ask = offerReplacePassphrase
			}
			got, found, _, err := reread.Get(replaceCredential, replaceRole, ask)
			if err != nil || !found || got != "synthetic-refresh-new" {
				t.Fatalf("Get() after the replacement = %q, %v, %v", got, found, err)
			}
			if _, found, _, _ := reread.Get(replaceCredential, "other_role", ask); found {
				t.Error("a replacement created a role")
			}
		})
	}
}

// An encrypted vault that is locked here cannot be replaced, and empty values are refused.
func TestReplaceRoleRefusals(t *testing.T) {
	lowWorkFactor(t)
	dir := t.TempDir()
	if err := New(dir).Set(replaceCredential, replaceRole, "synthetic-refresh-old", offerReplacePassphrase); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New(dir).ReplaceRole(replaceCredential, replaceRole, "synthetic-refresh-old", "synthetic-refresh-new"); !errors.Is(err, ErrNotUnlocked) {
		t.Errorf("ReplaceRole() on a locked vault = %v, want ErrNotUnlocked", err)
	}
	v := New(dir)
	if _, err := v.Unlock(replacePassphrase); err != nil {
		t.Fatal(err)
	}
	for _, values := range [][2]string{{"", "synthetic-refresh-new"}, {"synthetic-refresh-old", ""}} {
		if _, _, err := v.ReplaceRole(replaceCredential, replaceRole, values[0], values[1]); !errors.Is(err, ErrEmptyReplacement) {
			t.Errorf("ReplaceRole(%q, %q) = %v, want ErrEmptyReplacement", values[0], values[1], err)
		}
	}
	if _, _, err := New(t.TempDir()).ReplaceRole(replaceCredential, replaceRole, "a-value", "b-value"); err != nil {
		t.Errorf("ReplaceRole() on an absent vault = %v, want nothing replaced and no error", err)
	}
}

// A value queued while the vault was locked counts as stored: the replacement compares with it and merges it,
// so no later unlock merges it over the replacement.
func TestReplaceRoleMergesPendingEntries(t *testing.T) {
	lowWorkFactor(t)
	dir := t.TempDir()
	if err := New(dir).Set(replaceCredential, replaceRole, "synthetic-refresh-old", offerReplacePassphrase); err != nil {
		t.Fatal(err)
	}
	if err := New(dir).Set(replaceCredential, replaceRole, "synthetic-refresh-queued", nil); err != nil {
		t.Fatal(err)
	}
	v := New(dir)
	if _, err := v.Unlock(replacePassphrase); err != nil {
		t.Fatal(err)
	}
	// Queued after this process unlocked, as a person storing a consent while a vault process runs does.
	if err := New(dir).Set(replaceCredential, replaceRole, "synthetic-refresh-later", nil); err != nil {
		t.Fatal(err)
	}
	current, replaced, err := v.ReplaceRole(replaceCredential, replaceRole, "synthetic-refresh-queued", "synthetic-refresh-new")
	if err != nil || replaced || current != "synthetic-refresh-later" {
		t.Fatalf("ReplaceRole() against a value queued since = %q, %v, %v; want the queued value", current, replaced, err)
	}
	current, replaced, err = v.ReplaceRole(replaceCredential, replaceRole, "synthetic-refresh-later", "synthetic-refresh-new")
	if err != nil || !replaced || current != "synthetic-refresh-new" {
		t.Fatalf("ReplaceRole() = %q, %v, %v", current, replaced, err)
	}
	if pending, err := v.pendingFiles(); err != nil || len(pending) != 0 {
		t.Fatalf("pending entries after the replacement = %v, %v; want them merged", pending, err)
	}
	reread := New(dir)
	got, _, _, err := reread.Get(replaceCredential, replaceRole, offerReplacePassphrase)
	if err != nil || got != "synthetic-refresh-new" {
		t.Fatalf("Get() after a fresh unlock = %q, %v; want the replacement to survive", got, err)
	}
}

// Of many replacements based on the same old value, each through a vault value of its own as separate
// processes hold them, exactly one wins and every other one learns the winner's value.
func TestReplaceRoleParallelWritersOneWins(t *testing.T) {
	lowWorkFactor(t)
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%v", encrypted), func(t *testing.T) {
			dir := t.TempDir()
			offer := PassphraseFunc(nil)
			if encrypted {
				offer = offerReplacePassphrase
			}
			if err := New(dir).Set(replaceCredential, replaceRole, "synthetic-refresh-old", offer); err != nil {
				t.Fatal(err)
			}
			const writers = 8
			vaults := make([]*Vault, writers)
			for i := range vaults {
				vaults[i] = New(dir)
				if encrypted {
					if _, err := vaults[i].Unlock(replacePassphrase); err != nil {
						t.Fatal(err)
					}
				}
			}
			type outcome struct {
				current  string
				replaced bool
				err      error
			}
			outcomes := make([]outcome, writers)
			var wg sync.WaitGroup
			for i := range vaults {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					current, replaced, err := vaults[i].ReplaceRole(replaceCredential, replaceRole,
						"synthetic-refresh-old", fmt.Sprintf("synthetic-refresh-writer-%d", i))
					outcomes[i] = outcome{current, replaced, err}
				}(i)
			}
			wg.Wait()
			winner := ""
			for i, o := range outcomes {
				if o.err != nil {
					t.Fatalf("writer %d: %v", i, o.err)
				}
				if o.replaced {
					if winner != "" {
						t.Fatalf("two writers replaced the same old value")
					}
					winner = o.current
				}
			}
			if winner == "" {
				t.Fatal("no writer replaced the old value")
			}
			for i, o := range outcomes {
				if o.current != winner {
					t.Errorf("writer %d sees %q, want the winner's value", i, o.current)
				}
			}
			got, _, _, err := New(dir).Get(replaceCredential, replaceRole, offer)
			if err != nil || got != winner {
				t.Fatalf("stored value = %q, %v; want the winner's", got, err)
			}
		})
	}
}
