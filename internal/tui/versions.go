package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate"
)

// The version list (command version, and the Update part of Settings) lets a person install a newer
// release, a prerelease, or an older version. It shows the choice 'qconnect update --list' shows, installs a
// newer one or a prerelease like 'qconnect update --to', and treats an older one as the checked rollback of
// that command.

// versionUpdater is implemented by an Updater that can install a chosen version. Without it the editor
// offers no version list.
type versionUpdater interface {
	// Versions lists the releases that can be chosen, newest first.
	Versions(ctx context.Context) ([]selfupdate.Entry, error)
	// InstallVersion installs a newer release or a prerelease named by tag.
	InstallVersion(ctx context.Context, tag string, progress func(done, total int64)) (selfupdate.Result, error)
	// PrepareRollback downloads and verifies an older version named by tag and checks it against the
	// configuration. Nothing is changed.
	PrepareRollback(ctx context.Context, tag string, progress func(done, total int64)) (Rollback, error)
}

// Rollback is an older version that is downloaded, verified, and checked against the configuration.
type Rollback interface {
	// Reads reports that the older version reads the current configuration, so nothing needs restoring.
	Reads() bool
	// Backup names the newest backup the older version reads, and when it was made; ok is false where there
	// is none or where Reads is true.
	Backup() (name string, at time.Time, ok bool)
	// Install installs the older version, restoring the offered backup first when restore is true.
	Install(ctx context.Context, restore bool) (selfupdate.Result, error)
}

// versionsTimeout bounds the lookup of the list, which runs on the network while the editor waits.
const versionsTimeout = 20 * time.Second

type (
	// versionsLoadedMsg carries the list asked for from screen from.
	versionsLoadedMsg struct {
		entries []selfupdate.Entry
		err     error
		from    screen
	}
	// rollbackPreparedMsg carries the older version after its download and its check against the
	// configuration.
	rollbackPreparedMsg struct {
		entry selfupdate.Entry
		plan  Rollback
		err   error
	}
)

// versionChoice is the open version list.
type versionChoice struct {
	// from is the screen the list returns to.
	from screen
	// list shows entries by the index that is the entry ID.
	list    *kittui.List
	entries []selfupdate.Entry
}

// plain is the style of the running text of a question.
var plain = lipgloss.NewStyle()

// versionPending is the question about a chosen version: the installation itself, or, once the older
// version was checked, whether to restore a backup first (plan is then set).
type versionPending struct {
	entry selfupdate.Entry
	plan  Rollback
	// from is the screen the editor returns to once the list is closed.
	from screen
}

// versionsUnavailable is why the version list cannot open now, or empty where it can.
func (m *Model) versionsUnavailable() string {
	if _, ok := m.updater.(versionUpdater); !ok {
		return tr(tUpdateBuildCannot)
	}
	switch {
	case m.updating:
		return tr(tUpdateRunning)
	case m.updated != "":
		return tr(tUpdateAlreadyDone)
	case m.versionsLoading:
		return tr(tVersionsLoading)
	}
	return ""
}

// openVersions looks the list up in the background and opens it. It meets the blockers of u, because
// choosing a version ends in the same replacement.
func (m *Model) openVersions() tea.Cmd {
	m.clearMessages()
	if reason := m.versionsUnavailable(); reason != "" {
		m.status = reason
		return nil
	}
	if work := m.updateBlocker(); work != "" {
		m.status = tr(tVersionWaitAsk, work)
		return nil
	}
	updater := m.updater.(versionUpdater)
	from := m.screen
	m.versionsLoading = true
	m.status = tr(tVersionsLoading)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), versionsTimeout)
		defer cancel()
		entries, err := updater.Versions(ctx)
		return versionsLoadedMsg{entries: entries, err: err, from: from}
	}
}

// versionsLoaded opens the list. A list that arrives after the person moved on is dropped.
func (m *Model) versionsLoaded(msg versionsLoadedMsg) {
	m.versionsLoading = false
	if m.screen != msg.from {
		return
	}
	m.status = ""
	if msg.err != nil {
		m.fail = tr(tVersionsFailed, m.redactor.Error(msg.err))
		return
	}
	if len(msg.entries) == 0 {
		m.status = tr(tVersionsNone)
		return
	}
	// The cursor starts on the running version, so newer and older ones are a step away either way. A tag
	// comes from the network, so it is drawn through the log filter.
	items := make([]kittui.ListItem, len(msg.entries))
	current := 0
	for i, e := range msg.entries {
		if e.Current {
			current = i
		}
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: []string{m.logText(e.Version),
			strings.Join(versionMarks(e), " · ")}}
	}
	list, err := newChoice(kittui.ListSpec{Columns: []kittui.Column{{Title: columnID("version")},
		{Title: columnID("notes"), Dimmed: true}}, Search: kittui.SearchAsTyped, Empty: tVersionsNoMatch},
		tr(tVersionsTitle), items, current, "{enter} choose", "{back} close")
	if err != nil {
		m.fail = err.Error()
		return
	}
	m.versions = &versionChoice{from: msg.from, list: list, entries: msg.entries}
	m.screen = screenVersions
	m.syncChoice()
}

// closeVersions leaves the list.
func (m *Model) closeVersions() {
	if m.versions != nil {
		m.screen = m.versions.from
	}
	m.versions = nil
}

// versionMarks are the words that tell a version apart in the list.
func versionMarks(e selfupdate.Entry) []string {
	var marks []string
	if e.Current {
		marks = append(marks, tr(tVersionMarkRunning))
	}
	if !e.Current && !e.Older {
		marks = append(marks, tr(tVersionMarkNewer))
	}
	if e.Prerelease {
		marks = append(marks, tr(tVersionMarkPrerelease))
	}
	if e.Older {
		marks = append(marks, tr(tVersionMarkOlder))
	}
	return marks
}

// versionAnswer runs what the version list answers: enter asks about the marked version, which the list
// itself never installs, and back closes the list.
func (m *Model) versionAnswer(act action) tea.Cmd {
	v := m.versions
	if v == nil {
		return nil
	}
	switch act {
	case actEnter:
		if i, ok := chosen(v.list, len(v.entries)); ok {
			m.askVersion(v.entries[i])
		}
	case actBack:
		m.closeVersions()
	}
	return nil
}

// askVersion opens the question about installing e over the list.
func (m *Model) askVersion(e selfupdate.Entry) {
	m.clearMessages()
	if e.Current {
		m.status = tr(tVersionIsRunning, e.Version)
		return
	}
	if work := m.updateBlocker(); work != "" {
		m.status = tr(tVersionWaitAsk, work)
		return
	}
	m.versionPending = &versionPending{entry: e, from: m.versions.from}
	m.updateFrom = screenVersions
	m.screen = screenUpdate
	// The consequences are counted once here: the view is drawn on every key.
	m.updateText = consequencesOf(selfupdate.TargetQuestion(e.Version, e.Prerelease, e.Older))
}

// consequencesOf is the part of the core's question after the version, which says what the installation does
// to running qconnect processes; the question is built for a terminal and holds the version in front of it.
func consequencesOf(question string) string {
	if _, after, ok := strings.Cut(question, "? "); ok {
		return after
	}
	return question
}

// versionAsk asks before the installation, or, for an older version whose configuration check needs a
// backup, before the restore.
func (m *Model) versionAsk() ask {
	p := m.versionPending
	if p.plan != nil {
		return m.restoreAsk()
	}
	e := p.entry
	note := tr(tVersionNewerNote, m.runningVersion())
	switch {
	case e.Older && e.Prerelease:
		note = tr(tVersionOlderPrereleaseNote, m.runningVersion())
	case e.Older:
		note = tr(tVersionOlderNote, m.runningVersion())
	case e.Prerelease:
		note = tr(tVersionPrereleaseNote, m.runningVersion())
	}
	after := tr(tUpdateAfterManual)
	if m.restart != nil {
		after = tr(tUpdateAfterRestart)
	}
	return ask{
		title:        tAskVersion,
		question:     tr(tVersionQuestion, e.Version),
		consequence:  note,
		irreversible: e.Older,
		details: []kittui.DialogLine{{Text: strings.TrimSpace(m.updateText)}, {},
			{Text: after}},
		answers: cancelAnswers("{yes} install"),
		keep:    actNo,
	}
}

// restoreAsk asks whether to restore the backup the older version reads.
func (m *Model) restoreAsk() ask {
	p := m.versionPending
	name, at, _ := p.plan.Backup()
	version := p.entry.Version
	return ask{
		title:        tAskRestore,
		question:     tr(tVersionRestoreQuestion, version),
		consequence:  tr(tVersionRestoreConsequence, version),
		irreversible: true,
		details: []kittui.DialogLine{{Text: tr(tVersionRestoreOffer, name,
			at.UTC().Format("2006-01-02 15:04 UTC"), version)}},
		answers: cancelAnswers("{yes} restore and install"),
		keep:    actNo,
	}
}

// answerVersion answers the question about a chosen version.
func (m *Model) answerVersion(act action) tea.Cmd {
	p := m.versionPending
	if act != actYes {
		m.cancelVersionAsk()
		return nil
	}
	if work := m.updateBlocker(); work != "" {
		m.versionPending = nil
		m.screen = p.from
		m.versions = nil
		m.status = tr(tVersionWaitConfirm, work)
		return nil
	}
	m.versionPending = nil
	m.screen = p.from
	if p.plan != nil {
		return m.installRollback(p.entry, p.plan, true)
	}
	m.versions = nil
	return m.installVersion(p.entry)
}

// cancelVersionAsk answers the question with no. Declining the installation returns to the list; declining
// the restore ends the rollback, which changed nothing.
func (m *Model) cancelVersionAsk() {
	p := m.versionPending
	m.versionPending = nil
	if p.plan != nil {
		m.versions = nil
		m.screen = p.from
		m.status = tr(tVersionRestoreDeclined)
		return
	}
	m.screen = screenVersions
	m.status = tr(tUpdateNotInstalled)
}

// installVersion installs e in the background: a newer release or a prerelease at once, an older version
// after it was checked against the configuration.
func (m *Model) installVersion(e selfupdate.Entry) tea.Cmd {
	updater := m.updater.(versionUpdater)
	if e.Older {
		return m.runInstall(e.Version, func(progress func(done, total int64)) tea.Msg {
			plan, err := updater.PrepareRollback(context.Background(), e.Version, progress)
			return rollbackPreparedMsg{entry: e, plan: plan, err: err}
		})
	}
	return m.runInstall(e.Version, func(progress func(done, total int64)) tea.Msg {
		result, err := updater.InstallVersion(context.Background(), e.Version, progress)
		return updateDoneMsg{result: result, err: err}
	})
}

// installRollback installs the checked older version, restoring its backup first when restore is true.
func (m *Model) installRollback(e selfupdate.Entry, plan Rollback, restore bool) tea.Cmd {
	if restore {
		name, _, _ := plan.Backup()
		m.versionRestored = name
	}
	return m.runInstall(e.Version, func(func(done, total int64)) tea.Msg {
		result, err := plan.Install(context.Background(), restore)
		return updateDoneMsg{result: result, err: err}
	})
}

// rollbackPrepared goes on once the older version was downloaded and checked: it installs at once where the
// version reads the configuration, asks before restoring a backup where it does not, and stops where it
// reads neither.
func (m *Model) rollbackPrepared(msg rollbackPreparedMsg) tea.Cmd {
	m.updating = false
	if msg.err != nil {
		return m.updateDone(updateDoneMsg{err: msg.err})
	}
	e, plan := msg.entry, msg.plan
	if plan.Reads() {
		return m.installRollback(e, plan, false)
	}
	if _, _, ok := plan.Backup(); !ok {
		m.status = ""
		m.fail = tr(tVersionNoBackup, e.Version)
		return nil
	}
	m.versionPending = &versionPending{entry: e, plan: plan, from: m.screen}
	m.updateFrom = m.screen
	m.screen = screenUpdate
	m.clearMessages()
	return nil
}

// versionRestoredNote is what a successful rollback says about the backup it restored, once.
func (m *Model) versionRestoredNote() string {
	name := m.versionRestored
	m.versionRestored = ""
	if name == "" {
		return ""
	}
	return tr(tVersionRestored, name)
}
