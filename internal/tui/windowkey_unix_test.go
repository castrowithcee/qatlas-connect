//go:build linux || darwin

// A lock outside this window takes away the key the window unlocked itself, on the next tick, against a real
// vaultproc.Server in this test binary.
package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
)

// unlockedWindowWithProcess is a window that unlocked its encrypted vault with ctrl+l and handed it to a real
// vault process, with its admin session running.
func unlockedWindowWithProcess(t *testing.T, passphrase string) (*Model, string) {
	t.Helper()
	m, dir := earlierLockedModel(t, passphrase, nil)
	withStartableVaultProcess(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	unlockWithCtrlL(t, m, passphrase)
	if m.fail != "" {
		t.Fatalf("unlocking reported %q", m.fail)
	}
	if m.vaultSeenPID != os.Getpid() {
		t.Fatalf("vaultSeenPID = %d, want the vault process the unlock handed to (%d)", m.vaultSeenPID, os.Getpid())
	}
	if !m.AdminSessionActive() {
		t.Fatal("unlocking did not start the admin session")
	}
	return m, dir
}

// lockFromOutside locks the vault process the way 'qconnect vault lock' in another terminal does.
func lockFromOutside(t *testing.T, dir string) {
	t.Helper()
	if locked, err := vaultproc.LockProcess(context.Background(), vault.New(dir)); err != nil || !locked {
		t.Fatalf("LockProcess() = %v, %v, want the vault process locked", locked, err)
	}
}

// keyForgotten fails unless the window no longer holds the vault key nor an admin session.
func keyForgotten(t *testing.T, m *Model) {
	t.Helper()
	if state, err := m.secrets.Vault().State(); err != nil || state != vault.StateLocked {
		t.Errorf("Vault().State() = %v, %v, want the key forgotten", state, err)
	}
	if m.AdminSessionActive() {
		t.Error("the admin session is still active")
	}
	if m.status != tr(tVaultLockedElsewhere) {
		t.Errorf("status = %q, want %q", m.status, tr(tVaultLockedElsewhere))
	}
}

// The vault process is locked outside the window: the next tick forgets the window's own key, ends its admin
// session, says so, and Approvals shows the locked vault.
func TestALockOutsideTheWindowLocksItOnTheNextTick(t *testing.T) {
	const passphrase = "hunter2"
	m, dir := unlockedWindowWithProcess(t, passphrase)
	lockFromOutside(t, dir)

	deliver(m, vaultTick())
	keyForgotten(t, m)
	if got := shown(m.vaultHeaderText()); !strings.Contains(got, "vault locked") {
		t.Errorf("header = %q, want it locked", got)
	}
	openSectionByName(t, m, sectionApprovals)
	if view := screenOf(m); !strings.Contains(view, "The vault is locked") {
		t.Errorf("Approvals after the lock:\n%s", view)
	}
}

// Between two ticks the vault is locked and unlocked again elsewhere, by a vault process with another id: the
// window forgets its key and stands in the state only a vault process holds open.
func TestALockAndANewUnlockElsewhereLeaveTheProcessOnly(t *testing.T) {
	const passphrase = "hunter2"
	m, dir := unlockedWindowWithProcess(t, passphrase)
	lockFromOutside(t, dir)
	serveVaultProcess(t, dir, passphrase)
	original := vaultStatusFn
	t.Cleanup(func() { vaultStatusFn = original })
	vaultStatusFn = func(ctx context.Context, client *vaultproc.Client) (vaultproc.Status, error) {
		status, err := original(ctx, client)
		if err == nil {
			status.PID++
		}
		return status, err
	}

	deliver(m, vaultTick())
	keyForgotten(t, m)
	if !m.vaultProcessOnly() {
		t.Errorf("vault placed %s, want only the vault process holding it open", m.vaultPlace().Kind)
	}
	if got := shown(m.vaultHeaderText()); !strings.Contains(got, "vault unlocked") {
		t.Errorf("header = %q, want the vault unlocked for agents", got)
	}
}

// While the window's own hand-off runs, the vault process does not answer yet: neither that answer nor the
// same answer arriving once the hand-off is done takes the key away. Nor does a lock elsewhere while other
// vault work of the window is in flight; the first tick after it does.
func TestTheWindowKeepsItsKeyWhileItsOwnVaultWorkRuns(t *testing.T) {
	const passphrase = "hunter2"
	m, dir := earlierLockedModel(t, passphrase, nil)
	withStartableVaultProcess(t)
	var stale vaultProcessCheckMsg
	start := startVaultProcessFn
	startVaultProcessFn = func(ctx context.Context, path string, snap vault.Snapshot,
		client *vaultproc.Client) (vaultproc.Status, error) {
		if !m.vaultBusy {
			t.Error("the hand-off runs while the window is not busy")
		}
		stale = m.checkVaultProcess()().(vaultProcessCheckMsg)
		if !errors.Is(stale.err, vaultproc.ErrNotRunning) {
			t.Errorf("the ask during the hand-off = %v, want no vault process yet", stale.err)
		}
		deliver(m, func() tea.Msg { return stale })
		if state, err := m.secrets.Vault().State(); err != nil || state != vault.StateUnlocked {
			t.Errorf("Vault().State() during the hand-off = %v, %v, want the key kept", state, err)
		}
		return start(ctx, path, snap, client)
	}
	unlockWithCtrlL(t, m, passphrase)
	if m.fail != "" {
		t.Fatalf("unlocking reported %q", m.fail)
	}
	m.Update(stale)
	if state, err := m.secrets.Vault().State(); err != nil || state != vault.StateUnlocked {
		t.Fatalf("Vault().State() after the stale answer = %v, %v, want the key kept", state, err)
	}

	lockFromOutside(t, dir)
	m.vaultBusy = true
	deliver(m, vaultTick())
	if state, err := m.secrets.Vault().State(); err != nil || state != vault.StateUnlocked {
		t.Fatalf("Vault().State() while busy = %v, %v, want the key kept", state, err)
	}
	m.vaultBusy = false
	deliver(m, vaultTick())
	keyForgotten(t, m)
}

// An open form keeps what was typed into it through a lock elsewhere; saving it asks for the passphrase
// again, and the right one saves it.
func TestAFormKeepsItsInputThroughALockElsewhere(t *testing.T) {
	const passphrase = "hunter2"
	const url = "https://wiki.example.invalid"
	m, dir := unlockedWindowWithProcess(t, passphrase)
	openSectionByName(t, m, sectionServices)
	pressNew(t, m)
	typeText(t, m, "wiki-primary")
	press(t, m, "tab")
	press(t, m, "tab")
	typeText(t, m, url)

	lockFromOutside(t, dir)
	deliver(m, vaultTick())
	keyForgotten(t, m)
	if m.screen != screenForm {
		t.Fatalf("screen after the lock = %v, want the form", m.screen)
	}
	if view := screenOf(m); !strings.Contains(view, url) || !strings.Contains(view, "wiki-primary") {
		t.Fatalf("the form lost its input:\n%s", view)
	}

	press(t, m, "f2")
	if m.screen != screenAdminAuth || m.adminAuth == nil || !m.adminAuth.locked {
		t.Fatalf("saving after the lock = screen %v, want the passphrase for the locked vault", m.screen)
	}
	typeText(t, m, passphrase)
	pump(t, m, "enter")
	if m.fail != "" {
		t.Fatalf("saving with the passphrase reported %q", m.fail)
	}
	loaded, err := newTestStore(t, filepath.Join(dir, "config.yaml")).Load()
	if err != nil || loaded.Services["wiki-primary"].BaseURL != url {
		t.Fatalf("saved services = %+v, %v, want wiki-primary", loaded.Services, err)
	}
}
