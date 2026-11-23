package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

// maxUnlockWindow is a window over an encrypted vault, locked in its own process, whose settings hold
// maxUnlock, on a clock the test moves. No vault process takes the vault: the hand-off of this suite fails.
func maxUnlockWindow(t *testing.T, passphrase string, maxUnlock time.Duration) (*Model, *time.Time) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "qconnect")
	store := newTestStore(t, filepath.Join(dir, "config.yaml"))
	setup, _ := newVaultResolver(t, dir)
	mustNoError(t, setup.SetVault("other", "role", "canary-seed", func(string) (string, error) { return passphrase, nil }))
	settings := vault.DefaultSettings()
	settings.MaxUnlock = maxUnlock
	mustNoError(t, setup.Vault().SetSettings(settings))
	locked, _ := newVaultResolver(t, dir)
	runtimeDir, err := os.MkdirTemp("", "qv")
	if err != nil {
		t.Fatalf("MkdirTemp() = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	m, err := buildModel(store, nil, locked, nil)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	now := time.Date(2030, 3, 4, 10, 0, 0, 0, time.Local)
	m.now = func() time.Time { return now }
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	openSectionByName(t, m, sectionServices)
	pump(t, m, "ctrl+l")
	typeText(t, m, passphrase)
	pump(t, m, "enter")
	if state, err := m.secrets.Vault().State(); err != nil || state != vault.StateUnlocked {
		t.Fatalf("Vault().State() after ctrl+l = %v, %v, want unlocked", state, err)
	}
	return m, &now
}

// Without a vault process the window's own key ends after the maximum unlock duration, counted from its own
// unlock, and the header shows when, as it does for a vault process.
func TestTheMaximumUnlockDurationEndsAKeyWithoutAVaultProcess(t *testing.T) {
	const passphrase = "hunter2"
	m, now := maxUnlockWindow(t, passphrase, time.Hour)
	ends := now.Add(time.Hour)
	want := "vault unlocked until " + unlockDeadlineText(ends, *now)
	if got := shown(m.vaultHeaderText()); !strings.Contains(got, want) {
		t.Fatalf("header = %q, want it to contain %q", got, want)
	}

	*now = ends.Add(-time.Second)
	deliver(m, vaultTick())
	if state, err := m.secrets.Vault().State(); err != nil || state != vault.StateUnlocked {
		t.Fatalf("Vault().State() before the deadline = %v, %v, want unlocked", state, err)
	}

	*now = ends
	deliver(m, vaultTick())
	if state, err := m.secrets.Vault().State(); err != nil || state != vault.StateLocked {
		t.Fatalf("Vault().State() at the deadline = %v, %v, want the key forgotten", state, err)
	}
	if m.AdminSessionActive() {
		t.Error("the admin session outlived the key")
	}
	if m.status != tr(tVaultMaxUnlockReached) {
		t.Errorf("status = %q, want %q", m.status, tr(tVaultMaxUnlockReached))
	}
	if got := shown(m.vaultHeaderText()); !strings.Contains(got, "vault locked") {
		t.Errorf("header = %q, want it locked", got)
	}
}

// A maximum unlock duration of 0s keeps the window's key without a deadline.
func TestNoMaximumUnlockDurationKeepsTheKey(t *testing.T) {
	m, now := maxUnlockWindow(t, "hunter2", 0)
	*now = now.Add(1000 * time.Hour)
	deliver(m, vaultTick())
	if state, err := m.secrets.Vault().State(); err != nil || state != vault.StateUnlocked {
		t.Fatalf("Vault().State() = %v, %v, want unlocked", state, err)
	}
	if got := shown(m.vaultHeaderText()); strings.Contains(got, " until ") || !strings.Contains(got, "vault unlocked") {
		t.Errorf("header = %q, want unlocked without a deadline", got)
	}
}
