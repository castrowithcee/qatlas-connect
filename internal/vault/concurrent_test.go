package vault

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

const (
	rotCredential  = "consent-login"
	rotRole        = "refresh_token"
	rotOld         = "synthetic-refresh-old"
	rotNew         = "synthetic-refresh-rotated"
	concPassphrase = "synthetic-passphrase-concurrent"
	concNext       = "synthetic-passphrase-next"
)

// concEnv is a vault two processes share: a, which a person manages it through, unlocked where it is
// encrypted, and the directory a second process opens on its own.
type concEnv struct {
	dir        string
	encrypted  bool
	a          *Vault
	passphrase string
}

func newConcEnv(t *testing.T, encrypted bool) *concEnv {
	t.Helper()
	lowWorkFactor(t)
	e := &concEnv{dir: t.TempDir(), encrypted: encrypted, passphrase: concPassphrase}
	offer := PassphraseFunc(nil)
	if encrypted {
		offer = offering(concPassphrase)
	}
	if err := New(e.dir).Set(rotCredential, rotRole, rotOld, offer); err != nil {
		t.Fatal(err)
	}
	if err := New(e.dir).Set("wiki-reader", "token", "synthetic-token", nil); err != nil {
		t.Fatal(err)
	}
	e.a = New(e.dir)
	if encrypted {
		if _, err := e.a.Unlock(concPassphrase); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// rotate stores a rotated refresh token through a vault value of its own, as another process does.
func (e *concEnv) rotate(t *testing.T, name string) {
	t.Helper()
	b := New(e.dir)
	if e.encrypted {
		if _, err := b.Unlock(concPassphrase); err != nil {
			t.Fatal(err)
		}
	}
	old, found, _, err := b.Get(name, rotRole, nil)
	if err != nil || !found {
		t.Fatalf("Get() before the rotation = %v, %v", found, err)
	}
	if current, replaced, err := b.ReplaceRole(name, rotRole, old, rotNew); err != nil || !replaced || current != rotNew {
		t.Fatalf("ReplaceRole() = %q, %v, %v", current, replaced, err)
	}
}

// wantRotated checks that the rotated token is what the vault on disk holds for name, and what a reads.
func (e *concEnv) wantRotated(t *testing.T, name string) {
	t.Helper()
	got, found, _, err := New(e.dir).Get(name, rotRole, offering(e.passphrase))
	if err != nil || !found || got != rotNew {
		t.Errorf("on disk: %s = %q, %v, %v; want the rotated token", name, got, found, err)
	}
	got, found, _, err = e.a.Get(name, rotRole, nil)
	if err != nil || !found || got != rotNew {
		t.Errorf("read by the writer: %s = %q, %v, %v; want the rotated token", name, got, found, err)
	}
}

func forms(t *testing.T, encrypted, plain bool, run func(t *testing.T, e *concEnv)) {
	for _, enc := range []bool{false, true} {
		if enc && !encrypted || !enc && !plain {
			continue
		}
		t.Run(fmt.Sprintf("encrypted=%v", enc), func(t *testing.T) { run(t, newConcEnv(t, enc)) })
	}
}

// A process that holds the vault, unlocked where it is encrypted, while another process stores a rotated
// refresh token, writes the vault on disk under the write lock: whatever it writes, the rotation stays, on disk
// and in what it reads afterwards.
func TestEveryWriteKeepsATokenRotatedMeanwhile(t *testing.T) {
	cases := []struct {
		name             string
		encrypted, plain bool
		// prepare runs before the rotation and returns the write that follows it.
		prepare func(t *testing.T, e *concEnv) func() error
	}{
		{"set another role", true, true, func(t *testing.T, e *concEnv) func() error {
			return func() error { return e.a.Set(rotCredential, "client_id", "synthetic-client", nil) }
		}},
		{"set with id", true, true, func(t *testing.T, e *concEnv) func() error {
			return func() error {
				_, err := e.a.SetUndoableWithID("wiki-reader", "c0ffee00", "token", "synthetic-other", nil)
				return err
			}
		}},
		{"record credential ids", true, true, func(t *testing.T, e *concEnv) func() error {
			return func() error {
				recorded, _, err := e.a.RecordCredentialIDs(map[string]string{rotCredential: "c0ffee00"})
				if err == nil && recorded != 1 {
					err = fmt.Errorf("recorded %d ids, want 1", recorded)
				}
				return err
			}
		}},
		{"delete", true, true, func(t *testing.T, e *concEnv) func() error {
			return func() error {
				removed, err := e.a.Delete("wiki-reader", "token", nil)
				if err == nil && !removed {
					err = fmt.Errorf("nothing removed")
				}
				return err
			}
		}},
		{"approve", true, false, func(t *testing.T, e *concEnv) func() error {
			return func() error { return e.a.Approve([]Scope{wikiScope()}) }
		}},
		{"approve connections", true, false, func(t *testing.T, e *concEnv) func() error {
			return func() error {
				return e.a.ApproveConnections([]Scope{wikiScope()}, map[string]string{"wiki": "conn-id"})
			}
		}},
		{"revoke", true, false, func(t *testing.T, e *concEnv) func() error {
			approveScopes(t, e.a, wikiScope())
			return func() error { _, err := e.a.Revoke([]string{"wiki"}); return err }
		}},
		{"carry approval", true, false, func(t *testing.T, e *concEnv) func() error {
			approveScopes(t, e.a, wikiScope())
			return func() error { _, _, err := e.a.CarryApproval("wiki", "wiki-main", "conn-id"); return err }
		}},
		{"approve with token", true, false, func(t *testing.T, e *concEnv) func() error {
			approveScopes(t, e.a, wikiScope())
			token := createToken(t, e.a, "agent", "wiki")
			copied := wikiScope()
			copied.Connection = "wiki-copy"
			return func() error {
				result, err := e.a.ApproveWithToken(token.Value, []Scope{wikiScope(), copied}, nil, time.Now())
				if err == nil && (len(result.Changes) != 1 || !result.Changes[0].Approved) {
					err = fmt.Errorf("result = %+v, want the copy approved", result)
				}
				return err
			}
		}},
		{"rename credential", true, true, func(t *testing.T, e *concEnv) func() error {
			return func() error {
				_, _, err := e.a.RenameCredential(CredentialRename{From: "wiki-reader", To: "wiki-renamed", Entry: true})
				return err
			}
		}},
		{"clear rename marker", true, true, func(t *testing.T, e *concEnv) func() error {
			if _, _, err := e.a.RenameCredential(CredentialRename{From: "wiki-reader", To: "wiki-renamed", Entry: true,
				Mark: true}); err != nil {
				t.Fatal(err)
			}
			return func() error { return e.a.ClearRenameMarker("wiki-renamed") }
		}},
		{"encrypt", false, true, func(t *testing.T, e *concEnv) func() error {
			return func() error { return e.a.Encrypt(concPassphrase) }
		}},
		{"change passphrase", true, false, func(t *testing.T, e *concEnv) func() error {
			e.passphrase = concNext
			return func() error { return e.a.ChangePassphrase(concPassphrase, concNext) }
		}},
		{"decrypt", true, false, func(t *testing.T, e *concEnv) func() error {
			return func() error { return e.a.Decrypt(concPassphrase) }
		}},
		{"unlock merging a pending write", true, false, func(t *testing.T, e *concEnv) func() error {
			e.a.Forget()
			return func() error {
				// Queued after the rotation, which merged whatever was pending before it.
				if err := New(e.dir).Set("wiki-reader", "user", "synthetic-user", nil); err != nil {
					return err
				}
				merged, err := e.a.Unlock(concPassphrase)
				if err == nil && merged != 1 {
					err = fmt.Errorf("merged %d pending writes, want 1", merged)
				}
				return err
			}
		}},
		{"checkpoint restore", true, true, func(t *testing.T, e *concEnv) func() error {
			restore, err := e.a.Checkpoint()
			if err != nil {
				t.Fatal(err)
			}
			if err := e.a.Set(rotCredential, "client_id", "synthetic-client", nil); err != nil {
				t.Fatal(err)
			}
			return func() error {
				if err := e.a.Set("wiki-reader", "user", "synthetic-user", nil); err != nil {
					return err
				}
				return restore()
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forms(t, c.encrypted, c.plain, func(t *testing.T, e *concEnv) {
				write := c.prepare(t, e)
				e.rotate(t, rotCredential)
				if err := write(); err != nil {
					t.Fatalf("write = %v", err)
				}
				e.wantRotated(t, rotCredential)
			})
		})
	}
}

// An undo takes back its own write and nothing a rotation stored since, whether the rotation touched the same
// entry, the same role, or the entry under the name the undone write gave it.
func TestUndoKeepsATokenRotatedSince(t *testing.T) {
	cases := []struct {
		name             string
		encrypted, plain bool
		// write returns the undo, the name the rotation finds the token under, and a check after the undo.
		write func(t *testing.T, e *concEnv) (undo func() error, rotated string, check func(t *testing.T))
	}{
		{"set of another role", true, true, func(t *testing.T, e *concEnv) (func() error, string, func(*testing.T)) {
			undo, err := e.a.SetUndoable(rotCredential, "client_id", "synthetic-client", nil)
			if err != nil {
				t.Fatal(err)
			}
			return undo, rotCredential, func(t *testing.T) {
				if _, found, _, _ := New(e.dir).Get(rotCredential, "client_id", offering(concPassphrase)); found {
					t.Error("the undone role survived")
				}
			}
		}},
		{"set of the same role", true, true, func(t *testing.T, e *concEnv) (func() error, string, func(*testing.T)) {
			undo, err := e.a.SetUndoable(rotCredential, rotRole, "synthetic-refresh-consent", nil)
			if err != nil {
				t.Fatal(err)
			}
			return undo, rotCredential, func(*testing.T) {}
		}},
		{"record credential ids", true, true, func(t *testing.T, e *concEnv) (func() error, string, func(*testing.T)) {
			_, undo, err := e.a.RecordCredentialIDs(map[string]string{rotCredential: "c0ffee00"})
			if err != nil {
				t.Fatal(err)
			}
			return undo, rotCredential, func(t *testing.T) {
				if ids, _ := e.a.EntryCredentialIDs(); ids[rotCredential] != "" {
					t.Errorf("credential id = %q after the undo, want none", ids[rotCredential])
				}
			}
		}},
		{"carry approval", true, false, func(t *testing.T, e *concEnv) (func() error, string, func(*testing.T)) {
			approveScopes(t, e.a, wikiScope())
			_, undo, err := e.a.CarryApproval("wiki", "wiki-main", "conn-id")
			if err != nil {
				t.Fatal(err)
			}
			return undo, rotCredential, func(t *testing.T) {
				if approvals, _ := e.a.Approvals(); len(approvals) != 1 || approvals["wiki"].ConnectionID != "" {
					t.Errorf("approvals after the undo = %+v", approvals)
				}
			}
		}},
		{"rename credential", true, true, func(t *testing.T, e *concEnv) (func() error, string, func(*testing.T)) {
			_, undo, err := e.a.RenameCredential(CredentialRename{From: rotCredential, To: "consent-renamed", Entry: true})
			if err != nil {
				t.Fatal(err)
			}
			return undo, "consent-renamed", func(t *testing.T) {
				if held, _ := e.a.HasEntry("consent-renamed"); held {
					t.Error("the new name still holds an entry after the undo")
				}
			}
		}},
		{"checkpoint restore", true, true, func(t *testing.T, e *concEnv) (func() error, string, func(*testing.T)) {
			if e.encrypted {
				approveScopes(t, e.a, wikiScope())
			}
			restore, err := e.a.Checkpoint()
			if err != nil {
				t.Fatal(err)
			}
			if e.encrypted {
				if _, err := e.a.Revoke([]string{"wiki"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := e.a.SetUndoable("wiki-reader", "token", "synthetic-imported", nil); err != nil {
				t.Fatal(err)
			}
			return restore, rotCredential, func(t *testing.T) {
				if got, _, _, _ := New(e.dir).Get("wiki-reader", "token", offering(concPassphrase)); got != "synthetic-token" {
					t.Errorf("wiki-reader = %q after the restore, want its value from before", got)
				}
				if e.encrypted {
					if err := e.a.CheckApproval(wikiScope()); err != nil {
						t.Errorf("the revoked approval did not come back: %v", err)
					}
				}
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forms(t, c.encrypted, c.plain, func(t *testing.T, e *concEnv) {
				undo, rotated, check := c.write(t, e)
				e.rotate(t, rotated)
				if err := undo(); err != nil {
					t.Fatalf("undo = %v", err)
				}
				e.wantRotated(t, rotCredential)
				check(t)
			})
		})
	}
}

// A rotation this very vault value stores while its own checkpoint is open, as a token refresh during an
// import does, stays when the checkpoint is restored, and the checkpoint's writes before and after it are
// taken back.
func TestRestoreKeepsARotationOfTheSameVault(t *testing.T) {
	forms(t, true, true, func(t *testing.T, e *concEnv) {
		restore, err := e.a.Checkpoint()
		if err != nil {
			t.Fatal(err)
		}
		if err := e.a.Set("wiki-reader", "token", "synthetic-imported", nil); err != nil {
			t.Fatal(err)
		}
		if current, replaced, err := e.a.ReplaceRole(rotCredential, rotRole, rotOld, rotNew); err != nil ||
			!replaced || current != rotNew {
			t.Fatalf("ReplaceRole() = %q, %v, %v", current, replaced, err)
		}
		if err := e.a.Set("wiki-reader", "user", "synthetic-user", nil); err != nil {
			t.Fatal(err)
		}
		if err := restore(); err != nil {
			t.Fatalf("restore = %v", err)
		}
		e.wantRotated(t, rotCredential)
		reader := New(e.dir)
		if got, _, _, err := reader.Get("wiki-reader", "token", offering(concPassphrase)); err != nil ||
			got != "synthetic-token" {
			t.Errorf("wiki-reader token = %q, %v after the restore, want its value from before", got, err)
		}
		if _, found, _, err := reader.Get("wiki-reader", "user", nil); err != nil || found {
			t.Errorf("wiki-reader user = %v, %v after the restore, want it taken back", found, err)
		}
	})
}

// The undo of the vault's first write leaves a role that another process changed since.
func TestUndoOfTheFirstWriteKeepsARotation(t *testing.T) {
	lowWorkFactor(t)
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%v", encrypted), func(t *testing.T) {
			e := &concEnv{dir: t.TempDir(), encrypted: encrypted, passphrase: concPassphrase}
			e.a = New(e.dir)
			offer := PassphraseFunc(nil)
			if encrypted {
				offer = offering(concPassphrase)
			}
			undo, err := e.a.SetUndoable(rotCredential, rotRole, rotOld, offer)
			if err != nil {
				t.Fatal(err)
			}
			e.rotate(t, rotCredential)
			if err := undo(); err != nil {
				t.Fatal(err)
			}
			e.wantRotated(t, rotCredential)
		})
	}
}

// Management writes and rotations through vault values of their own, as separate processes hold them, run
// at the same time: no rotation is lost, every rotation is based on the one before it, and every management
// write is kept.
func TestManagementWritesAndRotationsInParallel(t *testing.T) {
	forms(t, true, true, func(t *testing.T, e *concEnv) {
		const (
			rotators  = 3
			managers  = 3
			rotations = 4
			writes    = 4
		)
		open := func() *Vault {
			v := New(e.dir)
			if e.encrypted {
				if _, err := v.Unlock(concPassphrase); err != nil {
					t.Error(err)
				}
			}
			return v
		}
		var mu sync.Mutex
		chain := map[string]string{} // old value -> the value that replaced it
		var wg sync.WaitGroup
		errs := make(chan error, 64)
		for i := 0; i < rotators; i++ {
			v := open()
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				old := rotOld
				for k := 0; k < rotations; {
					next := fmt.Sprintf("synthetic-refresh-%d-%d", i, k)
					current, replaced, err := v.ReplaceRole(rotCredential, rotRole, old, next)
					if err != nil {
						errs <- err
						return
					}
					if replaced {
						mu.Lock()
						if prior, ok := chain[old]; ok {
							errs <- fmt.Errorf("%q was replaced twice, by %q and %q", old, prior, next)
						}
						chain[old] = next
						mu.Unlock()
						k++
					}
					old = current
				}
			}(i)
		}
		for j := 0; j < managers; j++ {
			v := open()
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				for k := 0; k < writes; k++ {
					if err := v.Set("wiki-reader", fmt.Sprintf("role-%d", j), fmt.Sprintf("synthetic-%d-%d", j, k), nil); err != nil {
						errs <- err
						return
					}
					if e.encrypted {
						scope := wikiScope()
						scope.Connection = fmt.Sprintf("wiki-%d", j)
						if err := v.Approve([]Scope{scope}); err != nil {
							errs <- err
							return
						}
					}
				}
			}(j)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}

		if len(chain) != rotators*rotations {
			t.Fatalf("%d rotations recorded, want %d", len(chain), rotators*rotations)
		}
		last := rotOld
		for range chain {
			last = chain[last]
		}
		reader := open()
		if got, _, _, err := reader.Get(rotCredential, rotRole, nil); err != nil || got != last {
			t.Errorf("stored token = %q, %v; want the end of the rotation chain %q", got, err, last)
		}
		for j := 0; j < managers; j++ {
			want := fmt.Sprintf("synthetic-%d-%d", j, writes-1)
			if got, _, _, err := reader.Get("wiki-reader", fmt.Sprintf("role-%d", j), nil); err != nil || got != want {
				t.Errorf("role-%d = %q, %v; want %q", j, got, err, want)
			}
			if e.encrypted {
				scope := wikiScope()
				scope.Connection = fmt.Sprintf("wiki-%d", j)
				if err := reader.CheckApproval(scope); err != nil {
					t.Errorf("approval of %s lost: %v", scope.Connection, err)
				}
			}
		}
	})
}
