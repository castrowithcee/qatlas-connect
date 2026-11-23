package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
	"github.com/castrowithcee/qatlas-kit/i18n"
)

// noUnlockAdvice fails when a view calls the vault locked or names the lock key as the way to unlock it.
func noUnlockAdvice(t *testing.T, what, view string) {
	t.Helper()
	for _, bad := range []string{"The vault is locked", "vault locked", "unlock vault", "to unlock it", "enter unlock"} {
		if strings.Contains(view, bad) {
			t.Errorf("%s contains %q:\n%s", what, bad, view)
		}
	}
}

// A vault process holds the vault open while this window has no key: the header says unlocked, and the
// sections that need the key say so in their own words and offer the passphrase for this window; enter opens
// the passphrase dialog and, once it is answered, the list.
func TestApprovalsSectionWithOnlyTheVaultProcessUnlocked(t *testing.T) {
	const passphrase = "correct horse battery staple"
	dir := filepath.Join(t.TempDir(), "qconnect")
	store, _, _ := newApprovalFixture(t, dir, passphrase)

	m := openApprovalModel(t, store, dir)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	m.Update(processAnswer(m, vaultproc.Status{PID: 4100}, nil))
	openSectionByName(t, m, sectionApprovals)
	if m.screen == screenAdminAuth || m.adminAuth != nil {
		t.Fatal("merely opening the Approvals section asked for the passphrase")
	}
	if h := shown(m.vaultHeaderText()); !strings.Contains(h, "vault unlocked") {
		t.Fatalf("header = %q, want it to show unlocked", h)
	}
	view := screenOf(m)
	for _, want := range []string{
		"unlocked in the vault process, but not in this window",
		"to enter the passphrase for this window",
		"enter passphrase for this window",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the section lacks %q:\n%s", want, view)
		}
	}
	noUnlockAdvice(t, "the Approvals section", view)

	pump(t, m, "enter")
	unlockWithAdminPassphrase(t, m, passphrase)
	if m.screen != screenList || m.section != sectionApprovals {
		t.Fatalf("screen after the passphrase = %v section %v, want the Approvals list", m.screen, m.section)
	}
	if len(m.list.all) != 1 || m.list.all[0] != "personal" {
		t.Errorf("Approvals lists %v, want personal", m.list.all)
	}
}

func TestTokensSectionWithOnlyTheVaultProcessUnlocked(t *testing.T) {
	store, dir, v := newTokenFixture(t)
	fixtureToken(t, v, "kunde-a-ci", nil, "personal")
	m := openApprovalModel(t, store, dir)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	m.Update(processAnswer(m, vaultproc.Status{PID: 4100}, nil))

	openSectionByName(t, m, sectionTokens)
	view := screenOf(m)
	for _, want := range []string{"unlocked in the vault process, but not in this window", "enter passphrase for this window"} {
		if !strings.Contains(view, want) {
			t.Errorf("the section lacks %q:\n%s", want, view)
		}
	}
	noUnlockAdvice(t, "the Tokens section", view)
	if strings.Contains(view, "kunde-a-ci") {
		t.Errorf("the section lists a token without a key:\n%s", view)
	}

	pump(t, m, "enter")
	unlockWithAdminPassphrase(t, m, tokenPassphrase)
	if !slices.Equal(m.list.all, []string{"kunde-a-ci"}) {
		t.Fatalf("Tokens lists %v after the passphrase, want kunde-a-ci", m.list.all)
	}
}

func TestOverviewAndRenameSearchWithOnlyTheVaultProcessUnlocked(t *testing.T) {
	store, dir, _ := newTokenFixture(t)
	m := openApprovalModel(t, store, dir)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})

	m.Update(processAnswer(m, vaultproc.Status{PID: 4100}, nil))
	if got := m.unavailableWord(); got != tr(tOverviewVaultProcessOnly) {
		t.Errorf("overview word = %q, want %q", got, tr(tOverviewVaultProcessOnly))
	}
	m.section = sectionApprovals
	m.openFindings()
	if !strings.Contains(m.status, "unlocked in the vault process, but not in this window") || strings.Contains(m.status, "unlocks it") {
		t.Errorf("rename search status = %q", m.status)
	}
	noUnlockAdvice(t, "the rename search status", m.status)

	// The same window with no vault process keeps the old texts.
	m.Update(processAnswer(m, vaultproc.Status{}, vaultproc.ErrNotRunning))
	if got := m.unavailableWord(); got != tr(tOverviewVaultLocked) {
		t.Errorf("overview word locked = %q, want %q", got, tr(tOverviewVaultLocked))
	}
	m.openFindings()
	if !strings.Contains(m.status, "The vault is locked") {
		t.Errorf("rename search status locked = %q", m.status)
	}
}

// The real locked state, no vault process: the old text and key hint stay.
func TestApprovalsAndTokensKeepTheLockedTextWithoutAVaultProcess(t *testing.T) {
	store, dir, _ := newTokenFixture(t)
	m := openApprovalModel(t, store, dir)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	m.Update(processAnswer(m, vaultproc.Status{}, vaultproc.ErrNotRunning))
	for _, s := range []section{sectionApprovals, sectionTokens} {
		openSectionByName(t, m, s)
		view := screenOf(m)
		for _, want := range []string{"The vault is locked", "to unlock it", "enter unlock"} {
			if !strings.Contains(view, want) {
				t.Errorf("%v lacks %q:\n%s", s, want, view)
			}
		}
	}
}

// The key hint group for this state is translated like every other group of the key line.
func TestProcessOnlyKeyHintIsTranslated(t *testing.T) {
	store, dir, _ := newTokenFixture(t)
	for _, s := range []section{sectionApprovals, sectionTokens} {
		useLanguage(t, i18n.German)
		m := openApprovalModel(t, store, dir)
		m.Update(tea.WindowSizeMsg{Width: 200, Height: 28})
		m.Update(processAnswer(m, vaultproc.Status{PID: 4100}, nil))
		openSectionByName(t, m, s)
		view := screenOf(m)
		if !strings.Contains(view, "enter Passphrase für dieses Fenster") || strings.Contains(view, "passphrase for this window") {
			t.Errorf("%v: German key line lacks the translated group:\n%s", s, view)
		}
	}
}
