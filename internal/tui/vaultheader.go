// The vault state segment of the header, and 'ctrl+l', which unlocks or locks the vault from anywhere in
// the editor. See the header itself in layout.go (headerLines) and this window's own
// admin session in admin.go, which 'ctrl+l' starts or ends alongside the vault itself but never otherwise
// touches: the two stay independent.
package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
)

// vaultTickInterval is how often the header's vault state is refreshed from a vault process, and this
// window's admin abbreviation re-rendered against the wall clock. It is a variable only so a test can
// shrink it: deliver, the test suite's own way of draining a command's messages, calls a tea.Tick's
// function directly, which blocks for the interval, and a test never has a reason to wait out the real 5
// seconds.
var vaultTickInterval = 5 * time.Second

// vaultTickMsg drives the periodic refresh; vaultTick reschedules itself every time it fires.
type vaultTickMsg struct{}

func vaultTick() tea.Cmd {
	return tea.Tick(vaultTickInterval, func(time.Time) tea.Msg { return vaultTickMsg{} })
}

// vaultHeaderText is the vault state of the header as one text: symbol and word together, so it reads
// without colour and under NO_COLOR, plus the admin state for an encrypted, unlocked vault. "" for a run
// with no vault configured at all.
func (m *Model) vaultHeaderText() string { return strings.Join(m.vaultSegments(), headerSep) }

// vaultSegments are the status marks of the header: the vault, and for an encrypted, unlocked vault the
// admin session too. None for a run with no vault configured at all: showing a warning for something that
// does not exist yet would be misleading.
func (m *Model) vaultSegments() []string {
	var segments []string
	for _, mark := range m.vaultMarks() {
		segments = append(segments, statusMark(mark.kind, mark.text))
	}
	return segments
}

// headerMark is a status mark of the header as data: its kind and its words. A mark that tells what is wrong
// with the vault is urgent: it stands ahead of the keys at the right end of the header, so it stays in a
// narrow terminal.
type headerMark struct {
	kind   statusKind
	text   string
	urgent bool
}

// vaultMarks are the status marks of the header as data; vaultSegments draws them.
func (m *Model) vaultMarks() []headerMark {
	vs := m.vaultPlace()
	switch vs.Kind {
	case manage.VaultUnreadable:
		// Neither locked nor unlocked describes a vault whose state cannot even be read; this is closer to
		// the unencrypted warning than to either of the other two, and keeps the line naming what is wrong
		// instead of silently showing nothing.
		return []headerMark{{statusFail, tr(tVaultError, m.redactor.Apply(vs.Err.Error())), true}}
	case manage.VaultUnencrypted:
		return []headerMark{{statusWarn, tr(tVaultUnencrypted), true}}
	case manage.VaultProcessOnly, manage.VaultThisWindow:
		// A vault process elsewhere may hold it open while this window itself has never unlocked it; either
		// way the admin state belongs here too, since every unlocked, encrypted vault shows one, and
		// adminHeaderText already reads "admin off" (or the admin session time 0 text) on its own for a
		// window that never started a session, since AdminSessionActive is false regardless of why.
		admin := statusDim
		if m.adminTimeout() > 0 && m.AdminSessionActive() {
			admin = statusOK
		}
		unlocked := tr(tVaultUnlocked)
		if !vs.EndsAt.IsZero() {
			unlocked = tr(tVaultUnlockedUntil, unlockDeadlineText(vs.EndsAt, m.timeNow()))
		}
		return []headerMark{{statusOK, unlocked, false}, {admin, m.adminHeaderText(), false}}
	case manage.VaultLocked:
		return []headerMark{{statusWarn, tr(tVaultLocked), true}}
	}
	return nil
}

// vaultPlace places the vault by the one rule of the management core, from this window's cached view of the
// vault process (see checkVaultProcess) and its own hold on the key, so drawing never asks a socket.
func (m *Model) vaultPlace() manage.VaultState {
	vs := manage.VaultStateOf(m.secrets.Vault(), manage.VaultProcess{Unlocked: m.vaultProcessUnlocked, EndsAt: m.vaultProcessEndsAt})
	return vs.WithWindowKey(m.windowKey(vs), m.vaultMaxUnlock)
}

// windowKey is this window's own hold on the key of the vault placed at vs, as the management core decides
// from it.
func (m *Model) windowKey(vs manage.VaultState) manage.WindowKey {
	key := manage.WindowKey{Held: vs.Kind == manage.VaultThisWindow, SeenPID: m.vaultSeenPID, Busy: m.vaultBusy}
	if key.Held {
		key.Since = m.vaultKeySince
	}
	return key
}

// beginWindowKey starts this window's own hold on the key at its own unlock, with no vault process seen yet.
func (m *Model) beginWindowKey() {
	m.vaultKeySince, m.vaultSeenPID = m.timeNow(), 0
	m.vaultKeyGen++
}

// endWindowKey ends this window's own hold on the key.
func (m *Model) endWindowKey() {
	m.vaultKeySince, m.vaultSeenPID = time.Time{}, 0
	m.vaultKeyGen++
}

// noteWindowKey keeps the hold in step with the vault itself: a key this window came to hold without the
// unlock dialog, such as by turning on encryption, starts one, and a key it no longer holds ends it. A vault
// that cannot be read right now changes nothing.
func (m *Model) noteWindowKey() {
	vs := m.vaultPlace()
	held := vs.Kind == manage.VaultThisWindow
	switch {
	case held && m.vaultKeySince.IsZero():
		m.beginWindowKey()
	case !held && vs.Kind != manage.VaultUnreadable && !m.vaultKeySince.IsZero():
		m.endWindowKey()
	}
}

// vaultKeyTick is the vault's part of the periodic tick: a key this window holds past its maximum unlock
// duration, with no vault process seen since the unlock, is forgotten; otherwise the vault process is asked.
func (m *Model) vaultKeyTick() tea.Cmd {
	m.noteWindowKey()
	if m.windowKey(m.vaultPlace()).Expired(m.vaultMaxUnlock, m.timeNow()) {
		return m.forgetWindowKey(tr(tVaultMaxUnlockReached))
	}
	return m.checkVaultProcess()
}

// forgetWindowKey takes this window's own key away after a lock it did not ask for: the same steps as its
// own lock (see beginVaultLock and handleVaultLocked) without asking the vault process anything. Open forms
// and dialogs stay as they are; saving asks for the passphrase again through requireAdmin.
func (m *Model) forgetWindowKey(status string) tea.Cmd {
	if v := m.secrets.Vault(); v != nil {
		v.Forget()
	}
	m.endWindowKey()
	m.endAdminSession()
	m.RefreshAdminTimeout()
	m.refreshLogRetention()
	m.status = status
	return tea.Batch(m.reloadShownLogs(), m.loadOverviewActivity())
}

// vaultUnencrypted reports whether the vault exists without a passphrase, the state the header warns about.
func (m *Model) vaultUnencrypted() bool { return m.vaultPlace().Kind == manage.VaultUnencrypted }

// adminHeaderText is the header's admin abbreviation for an encrypted, unlocked vault: "admin off" while
// this window has no session, "admin Nm left" with its remaining time rounded to minutes, or "admin: confirm
// each change" for an admin session time of 0, which never keeps one at all (see requireAdmin in admin.go).
func (m *Model) adminHeaderText() string {
	if m.adminTimeout() <= 0 {
		return tr(tAdminEachChange)
	}
	if !m.AdminSessionActive() {
		return tr(tAdminOff)
	}
	minutes := int(m.AdminSessionRemaining().Round(time.Minute) / time.Minute)
	return tr(tAdminLeft, minutes)
}

// vaultLockKeyHint is the 'ctrl+l' group of the key line on the tab line and the lists, only for an
// encrypted vault, since an absent or unencrypted one has nothing to lock or unlock (handleVaultLockKey says
// so itself if it is pressed anyway).
func (m *Model) vaultLockKeyHint() string {
	switch vs := m.vaultPlace(); {
	case vs.Unlocked():
		return footerPlaceholder(actLock) + " lock vault"
	case vs.Kind == manage.VaultLocked:
		return footerPlaceholder(actLock) + " unlock vault"
	}
	return ""
}

// vaultLockKeyAllowed reports whether 'ctrl+l' acts on the screen the editor is on right now: every
// browsing screen, never one that already asks its own question (a masked prompt, a confirmation, or the
// leave question) and never this file's own two dialogs while they are already open. It works in lists and
// forms and every other screen besides those questions.
func (m *Model) vaultLockKeyAllowed() bool {
	switch m.screen {
	case screenNav, screenList, screenForm, screenTargets, screenPaths, screenPicker, screenProviders,
		screenSummary, screenLogs, screenLogDetail:
		return true
	}
	return false
}

// handleVaultLockKey runs 'ctrl+l' with no other dialog open (see vaultLockKeyAllowed, its only caller):
// gated first behind vaultBusy, since vault work already in flight must not be raced by a second attempt,
// exactly like every other vault action of this editor, it then opens the dialog the displayed state calls
// for (see vaultPlace, which the header itself also follows, so the two never contradict each
// other), or says there is nothing to do for one that is absent or unencrypted.
func (m *Model) handleVaultLockKey() tea.Cmd {
	if m.vaultBusy {
		m.status = tr(tVaultWriteBusy)
		return nil
	}
	vs := m.vaultPlace()
	switch {
	case vs.Kind == manage.VaultUnreadable:
		m.fail = m.redactor.Apply(vs.Err.Error())
	case vs.Unlocked():
		m.openVaultLockConfirm(m.screen)
	case vs.Kind == manage.VaultLocked:
		m.openVaultUnlockPrompt(m.screen)
	case m.secrets.Vault() == nil:
		m.status = tr(tVaultNone)
	default:
		m.status = tr(tVaultNothingToLock)
	}
	return nil
}

// vaultUnlockPrompt is the masked passphrase dialog 'ctrl+l' opens for a locked vault with no other action
// pending (see handleVaultLockKey). It is deliberately its own small dialog rather than a third mode of
// admin.go's adminAuth: unlike requireAdmin's "Unlock vault to continue", answering it does nothing but the
// unlock itself, the vault process handoff on Linux, macOS, and Windows (see startVaultProcess below), and starting this
// window's admin session; back is the screen it returns to either way, whichever one 'ctrl+l' was pressed
// from.
type vaultUnlockPrompt struct {
	input textinput.Model
	back  screen
}

func (m *Model) openVaultUnlockPrompt(back screen) {
	in := newTextInput()
	in.Prompt = ""
	in.EchoMode = textinput.EchoPassword
	in.Focus()

	m.vaultUnlock = &vaultUnlockPrompt{input: in, back: back}
	m.screen = screenVaultUnlock
	m.clearMessages()
}

// updateVaultUnlockPrompt handles the masked entry of the 'ctrl+l' unlock dialog.
func (m *Model) updateVaultUnlockPrompt(key tea.KeyPressMsg) tea.Cmd {
	u := m.vaultUnlock
	switch {
	case keyIs(key, actBack):
		back := u.back
		u.input.Reset()
		m.vaultUnlock = nil
		m.screen = back
		m.status = tr(tCancelled)
		return nil
	case keyIs(key, actEnter):
		if m.vaultBusy {
			// The scrypt work of an earlier attempt on this very dialog is still running.
			return nil
		}
		typed := u.input.Value()
		u.input.Reset()
		m.clearMessages()
		if typed == "" {
			m.fail = tr(tPassphraseEmpty)
			return nil
		}
		return m.beginVaultUnlock(typed, u.back)
	}
	var cmd tea.Cmd
	u.input, cmd = u.input.Update(inputMsg(key))
	return cmd
}

// vaultUnlockPrompt is the masked 'ctrl+l' unlock dialog, in the pattern of every other masked
// prompt this editor already shows (see adminAuthPrompt, vaultOfferPrompt): a title, the masked line, an
// explanation, and enter/esc. The explanation is its own text (see vaultUnlockHint), not admin.go's
// adminSessionHint: this dialog opens without a pending admin-gated action, so it reads as unlocking that
// also starts admin mode, not the other way round.
func (m *Model) vaultUnlockPrompt() promptDialog {
	d := promptDialog{title: tr(tUnlockTitle), input: m.inputLine("  ", m.vaultUnlock.input), field: true,
		hint: m.vaultUnlockHint(), keys: "{enter} unlock · {back} cancel"}
	if m.restartNote != "" {
		d.notice = m.indented(m.restartNote)
	}
	return d
}

// vaultUnlockHint explains what answering the 'ctrl+l' unlock dialog does besides unlocking the vault
// itself: starts this window's admin session too. The dialog opens for a locked vault, whose admin session
// time is stored in settings.age and unreadable until the passphrase unlocked it, so the hint must not
// claim a duration, nor that every change asks again.
func (m *Model) vaultUnlockHint() string { return tr(tUnlockAlsoAdmin) }

// vaultUnlockedMsg carries the outcome of unlocking the vault back into the event loop: the scrypt work
// vault.Vault.Unlock does never runs on it. back is the screen to return to; merged is how many pending
// entries were merged into the vault, the same count 'qconnect vault unlock' itself reports.
type vaultUnlockedMsg struct {
	err    error
	back   screen
	merged int
}

// beginVaultUnlock unlocks the vault with passphrase, as a command so the scrypt work never runs on the
// event loop; vaultBusy blocks a second vault write, or another such dialog, from starting before it is
// done.
func (m *Model) beginVaultUnlock(passphrase string, back screen) tea.Cmd {
	v := m.secrets.Vault()
	if v == nil {
		m.fail = tr(tVaultNone)
		return nil
	}
	m.vaultBusy = true
	m.busy = tr(tBusyUnlocking)
	return func() tea.Msg {
		merged, err := v.Unlock(passphrase)
		return vaultUnlockedMsg{err: err, back: back, merged: merged}
	}
}

// handleVaultUnlocked decides what unlocking the vault answers. A wrong passphrase reopens the very same
// dialog with the error shown, the same pattern every other masked prompt of this editor follows. The right
// one unlocks the vault in this process at once, starts this window's admin session too (the same passphrase
// just proved both, the same reasoning requireAdmin's own locked case already follows), and, only then, hands
// the vault to a vault process on Linux, macOS, and Windows in the background (see startVaultProcess): that hand-off can take a
// few seconds and must never delay returning control of the editor.
func (m *Model) handleVaultUnlocked(msg vaultUnlockedMsg) tea.Cmd {
	if msg.err != nil {
		m.vaultBusy, m.busy = false, ""
		if errors.Is(msg.err, vault.ErrWrongPassphrase) {
			m.openVaultUnlockPrompt(msg.back)
			m.fail = tr(tPassphraseWrong)
			return nil
		}
		m.vaultUnlock = nil
		m.screen = msg.back
		m.fail = m.redactor.Apply(msg.err.Error())
		return nil
	}
	m.vaultUnlock = nil
	m.screen = msg.back
	m.startAdminSession()
	m.status = tr(tVaultNowUnlocked)
	m.beginSettingsTakeover()
	if m.section == sectionVault && (m.screen == screenForm || m.pagePreview()) && m.formState() == m.pristine {
		// The update behaviour row can only be read now; rebuilding drops nothing, the form is unedited.
		back := m.screen
		m.openVaultForm()
		m.screen = back
	}
	m.vaultProcessUnlocked, m.vaultProcessEndsAt = false, time.Time{}
	m.beginWindowKey()
	if cmd := m.startVaultProcess(); cmd != nil {
		return cmd
	}
	m.vaultBusy, m.busy = false, ""
	return nil
}

// vaultProcessStartedMsg carries the outcome of handing the vault to a vault process back into the event
// loop; see startVaultProcess.
type vaultProcessStartedMsg struct {
	err error
	// pid is the vault process that holds the vault after the hand-off, 0 if none does.
	pid int
	// ended is the id of a vault process with a replaced program that was ended first, 0 if none was.
	ended int
	// earlier are the ids of vault processes of the release before qatlas-connect that were ended first.
	earlier []int
}

// endEarlierFn ends the vault processes of the release before qatlas-connect that hold the vault
// unlocked: manage.Service.EndEarlierProcesses for a real run. A test replaces it, since only a helper
// process of that release can stand in for one and no such helper exists in this package.
var endEarlierFn = func(ctx context.Context, svc *manage.Service, v *vault.Vault) ([]int, error) {
	return svc.EndEarlierProcesses(ctx, v)
}

// earlierEndedText is the status text for each vault process of the earlier release that was ended.
func earlierEndedText(pids []int) []string {
	var parts []string
	for _, pid := range pids {
		parts = append(parts, tr(tVaultEarlierEnded, pid))
	}
	return parts
}

// startVaultProcessFn is what startVaultProcess calls to actually hand the vault to a new vault process
// once none answers yet: vaultproc.StartProcess for a real run, which starts a second copy of the running
// program. A test replaces it with one that starts an in-process vaultproc.Server instead, the same way
// package cli keeps 'vault unlock' itself from starting a second copy of its own test binary (see
// runAsQatlasEnv there); everything around this seam, checking whether a process already answers and
// building the snapshot, still runs for real.
var startVaultProcessFn = vaultproc.StartProcess

// startVaultProcess hands the just-unlocked vault to a vault process on Linux, macOS, and Windows, exactly the way 'qconnect
// vault unlock' does (see vaultproc.StartProcess, the shared core both run): nil where this platform
// runs no vault process at all, so the caller never has to ask vaultproc.Supported itself. A process already
// running elsewhere, or none running yet because this platform never starts one, stays silent; any other
// failure becomes a warning with secret.VaultProcessRemedy, never with a secret value, the same rule
// manage.Service.SyncVaultProcess and LockVaultProcess already follow.
func (m *Model) startVaultProcess() tea.Cmd {
	if !vaultproc.Supported {
		return nil
	}
	v := m.secrets.Vault()
	if v == nil {
		return nil
	}
	configPath, err := filepath.Abs(m.svc.Path())
	if err != nil {
		return func() tea.Msg { return vaultProcessStartedMsg{err: err} }
	}
	m.busy = tr(tBusyVaultProcess)
	return func() tea.Msg {
		ended := 0
		client, err := vaultproc.ProcessClientOf(v)
		if err != nil {
			return vaultProcessStartedMsg{err: err}
		}
		if status, err := client.Status(context.Background()); err == nil {
			// Another window, or another terminal's 'qconnect vault unlock', already started one: nothing more
			// to do.
			return vaultProcessStartedMsg{pid: status.PID}
		} else if errors.Is(err, vaultproc.ErrReplaced) {
			// Its program was replaced and no successor took over: it is ended, as 'qconnect vault lock'
			// does, and a new one started with the vault unlocked here.
			pid, endErr := vaultproc.EndReplaced(context.Background(), client, err)
			if endErr != nil {
				return vaultProcessStartedMsg{err: endErr}
			}
			ended = pid
		} else if !errors.Is(err, vaultproc.ErrNotRunning) {
			return vaultProcessStartedMsg{err: err}
		}
		// This release cannot ask a vault process of the earlier one; it is ended before a new one starts,
		// so the vault is never held by two processes.
		earlier, err := endEarlierFn(context.Background(), m.svc, v)
		if err != nil {
			return vaultProcessStartedMsg{err: err, ended: ended, earlier: earlier}
		}
		snap, err := v.Snapshot()
		if err != nil {
			return vaultProcessStartedMsg{err: err, ended: ended, earlier: earlier}
		}
		status, err := startVaultProcessFn(context.Background(), configPath, snap, client)
		return vaultProcessStartedMsg{err: err, pid: status.PID, ended: ended, earlier: earlier}
	}
}

// handleVaultProcessStarted reports a failed hand-off as a warning, never blocking anything the unlock
// itself already finished: the vault stays unlocked in this process either way. The process it handed to is
// the first one this window has seen since its unlock; an answer asked during the hand-off is dropped.
func (m *Model) handleVaultProcessStarted(msg vaultProcessStartedMsg) tea.Cmd {
	m.vaultBusy, m.busy = false, ""
	m.vaultKeyGen++
	m.vaultSeenPID = m.windowKey(m.vaultPlace()).Seen(vaultproc.Status{PID: msg.pid}, msg.err).SeenPID
	var ended []string
	if msg.ended > 0 {
		ended = append(ended, tr(tVaultProcessEnded, msg.ended))
	}
	ended = append(ended, earlierEndedText(msg.earlier)...)
	if msg.err == nil {
		if len(ended) > 0 {
			m.status = strings.Join(ended, "; ")
		}
		return nil
	}
	warning := tr(tVaultProcessNotStarted, m.redactor.Apply(msg.err.Error()), secret.VaultProcessRemedy(msg.err))
	m.status = strings.Join(append(ended, warning), "; ")
	return nil
}

// openVaultLockConfirm opens the short confirmation 'ctrl+l' asks before locking an unlocked, encrypted
// vault (see handleVaultLockKey); back is the screen it returns to either way.
func (m *Model) openVaultLockConfirm(back screen) {
	m.vaultLockConfirm, m.vaultLockBack = true, back
	m.screen = screenConfirm
	m.clearMessages()
}

// vaultLockConfirmView draws the "Lock the vault now?" question.
func (m *Model) vaultLockConfirmView() string {
	return m.questionText(question{
		title:       tr(tLockTitle),
		consequence: tr(tLockConsequence),
		keys:        "{yes} lock · {no}/{back} stay unlocked · {command} cmds · {help} help",
	})
}

// vaultLockedMsg carries the outcome of locking the vault back into the event loop.
type vaultLockedMsg struct {
	err error
	// earlier are the ids of vault processes of the release before qatlas-connect that were ended.
	earlier []int
}

// beginVaultLock runs the three things 'ctrl+l' answered "y" does: locks the vault process that
// holds it unlocked outside this run, the same way 'qconnect vault lock' does (see vaultproc.LockProcess,
// the shared core both run); forgets the key and document this process itself holds, the closest the vault
// API comes to undoing Unlock (see vault.Vault.Forget); and, once the command returns, ends this window's
// admin session (see handleVaultLocked), since managing without an unlocked vault is not possible anyway.
// Both vault actions run as a command: locking a running process is a socket round trip, never the event
// loop's job.
func (m *Model) beginVaultLock() tea.Cmd {
	v := m.secrets.Vault()
	if v == nil {
		return nil
	}
	m.vaultBusy = true
	m.busy = tr(tBusyLocking)
	return func() tea.Msg {
		_, err := vaultproc.LockProcess(context.Background(), v)
		// Locking only takes access away, so a vault process of the earlier release is ended whatever became
		// of this release's own, as 'qconnect vault lock' does.
		earlier, endErr := endEarlierFn(context.Background(), m.svc, v)
		v.Forget()
		return vaultLockedMsg{err: errors.Join(err, endErr), earlier: earlier}
	}
}

// handleVaultLocked applies the outcome of beginVaultLock. The key is forgotten and the admin session ended
// either way: both are this window's own state, and neither depends on whether a remote vault process could
// be reached to lock as well.
func (m *Model) handleVaultLocked(msg vaultLockedMsg) tea.Cmd {
	m.vaultBusy, m.busy = false, ""
	m.endWindowKey()
	m.endAdminSession()
	m.RefreshAdminTimeout()
	m.vaultProcessUnlocked, m.vaultProcessEndsAt = false, time.Time{}
	if msg.err != nil {
		m.fail = tr(tVaultProcessNotLocked, m.redactor.Apply(msg.err.Error()), secret.VaultProcessRemedy(msg.err))
		return nil
	}
	m.status = strings.Join(append([]string{tr(tVaultNowLocked)}, earlierEndedText(msg.earlier)...), "; ")
	return nil
}

// unlockDeadlineText is the local time of t, with the date only when it is not today.
func unlockDeadlineText(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("2006-01-02 15:04")
}

// vaultProcessCheckMsg carries the answer of the vault process to checkVaultProcess: its status, or why there
// was none. It holds no secret and is never used to read or write one. gen is vaultKeyGen when it was asked.
type vaultProcessCheckMsg struct {
	status vaultproc.Status
	err    error
	gen    int
}

// vaultStatusFn asks the vault process for its status: vaultproc.Client.Status for a real run. A test
// replaces it to stand in for a process with another id, since every in-process server reports the id of
// the test binary.
var vaultStatusFn = func(ctx context.Context, client *vaultproc.Client) (vaultproc.Status, error) {
	return client.Status(ctx)
}

// checkVaultProcess asks, off the event loop, whether a vault process holds the vault open right now, for
// every encrypted vault on a platform that runs a vault process at all: the header shows "unlocked" for a
// vault only a process holds open, and a window that holds the key itself learns of a lock elsewhere (see
// handleVaultProcessCheck). Every other case returns nil, so vaultTick's own tea.Batch carries nothing for
// it that tick.
func (m *Model) checkVaultProcess() tea.Cmd {
	v := m.secrets.Vault()
	if v == nil || !vaultproc.Supported {
		return nil
	}
	state, err := v.State()
	if err != nil || state != vault.StateLocked && state != vault.StateUnlocked {
		return nil
	}
	gen := m.vaultKeyGen
	return func() tea.Msg {
		client, err := vaultproc.ProcessClientOf(v)
		if err != nil {
			return vaultProcessCheckMsg{err: err, gen: gen}
		}
		status, err := vaultStatusFn(context.Background(), client)
		return vaultProcessCheckMsg{status: status, err: err, gen: gen}
	}
}

// handleVaultProcessCheck applies an answer of the vault process: to the header, and to this window's own
// key, which the management core decides is locked elsewhere or has now seen its vault process. An answer
// asked before this window's hold on the key last changed is dropped; the next tick asks again.
func (m *Model) handleVaultProcessCheck(msg vaultProcessCheckMsg) tea.Cmd {
	if msg.gen != m.vaultKeyGen {
		return nil
	}
	m.vaultProcessUnlocked, m.vaultProcessEndsAt = msg.err == nil, msg.status.UnlockEndsAt
	key := m.windowKey(m.vaultPlace())
	if manage.LockedElsewhere(key, msg.status, msg.err) {
		return m.forgetWindowKey(tr(tVaultLockedElsewhere))
	}
	m.vaultSeenPID = key.Seen(msg.status, msg.err).SeenPID
	return nil
}

// lockUnavailable is why ctrl+l would not lock the vault now.
func (m *Model) lockUnavailable() string {
	if !m.vaultLockKeyAllowed() {
		return tr(tLockCannotHere)
	}
	vs := m.vaultPlace()
	switch {
	case vs.Kind == manage.VaultUnreadable:
		return tr(tLockStateUnknown)
	case vs.Unlocked():
		return ""
	case vs.Kind == manage.VaultLocked:
		return tr(tLockAlready)
	}
	if m.secrets.Vault() == nil {
		return tr(tVaultNone)
	}
	return tr(tVaultNothingToLock)
}
