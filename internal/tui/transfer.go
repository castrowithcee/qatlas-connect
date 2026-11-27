package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-kit/projectpath"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// Exporting the configuration is ':export'. The editor only asks what to take, how to protect the secrets,
// and where to write; the preview, the secret reading and the file are manage.Service's. It is a managing
// action like every other one, so it runs behind the admin session, and the export with secrets runs only
// where the vault has a passphrase: there is no admin session to prove otherwise.

// exportStep is the question an export is at.
type exportStep int

const (
	exportSelect exportStep = iota
	exportMode
	exportPassphrase
	exportPlainConfirm
	exportTarget
	exportPreview
	exportResult
)

// exportKind says whether and how the file holds secrets.
type exportKind int

const (
	exportWithout exportKind = iota
	exportEncrypted
	exportUnencrypted
)

// exportModeRows are the answers of the mode step, in the order of exportKind.
func exportModeRows() []string {
	return []string{tr(tExportModeNone), tr(tExportModeEncrypted), tr(tExportModePlain)}
}

const (
	exportDefaults = "defaults"
	exportNotes    = "notes"
	exportConnPref = "connection:"
	exportSvcPref  = "service:"
	exportCredPref = "credential:"
	exportFileName = "qatlas-export.yaml"
)

// exportState is the export in progress, while screenTransfer shows it.
type exportState struct {
	step exportStep
	// from is the screen the export was started from and returns to.
	from screen
	// entries are the keys of what can be ticked; the ID of a row of list is its index there, since a name may
	// hold what an ID may not. The ticks are the list's.
	entries []string
	list    *kittui.List
	// mode is the list of the mode step; secretsReason is why the rows with secrets are not possible ("" where
	// they are), asked once when the step is entered because it reads the vault state.
	mode          *kittui.List
	kind          exportKind
	secretsReason string
	// first is the first passphrase entry while it is typed again; passphrase is the confirmed one. Both
	// only ever reach the core's ExportPassphrase and are cleared when the export ends.
	first, passphrase string
	confirming        bool
	target            string
	// lines and view are the preview or the result, one line each, and the scroll position in them.
	lines []string
	view  textScroll
	// writing is true while the core writes the file; no key acts then.
	writing bool
}

// exportDoneMsg carries the outcome of the write back into the event loop.
type exportDoneMsg struct {
	res  *manage.ExportResult
	err  error
	path string
}

// askExport starts the export, once the admin session allows it.
func (m *Model) askExport() tea.Cmd {
	if work := m.updateBlocker(); work != "" {
		m.clearMessages()
		m.status = tr(tExportWait, work)
		return nil
	}
	return m.requireAdmin(func() tea.Cmd { m.openExport(); return nil })
}

func (m *Model) openExport() {
	entries := []string{exportDefaults, exportNotes}
	for _, kind := range []struct {
		prefix string
		keys   func() []string
	}{
		{exportConnPref, func() []string { return slices.Sorted(maps.Keys(m.cfg.Connections)) }},
		{exportSvcPref, func() []string { return slices.Sorted(maps.Keys(m.cfg.Services)) }},
		{exportCredPref, func() []string { return slices.Sorted(maps.Keys(m.cfg.Credentials)) }},
	} {
		for _, name := range kind.keys() {
			entries = append(entries, kind.prefix+name)
		}
	}
	list, err := m.exportList(entries)
	if err != nil {
		m.clearMessages()
		m.fail = err.Error()
		return
	}
	m.export = &exportState{from: m.screen, entries: entries, list: list}
	m.screen = screenTransfer
	m.clearMessages()
}

// exportList is the kit list of what can be ticked: a tick box and the row of all, the name first and the
// kind beside it. The names are the configuration's and untrusted: they stand through logText.
func (m *Model) exportList(entries []string) (*kittui.List, error) {
	list, err := kittui.NewList(kittui.ListSpec{
		Columns: []kittui.Column{{Title: columnID("NAME")}, {Title: columnID("KIND"), Dimmed: true}},
		Keys:    listKeys(actSpace), Search: kittui.SearchByKey, Ticks: true, AllRow: true,
		Empty: tFormNothingConfiguredYet}, keymap, translator(false))
	if err != nil {
		return nil, err
	}
	items := make([]kittui.ListItem, len(entries))
	for i, key := range entries {
		name, kind := m.exportRowText(key)
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: []string{name, kind}}
	}
	return list, list.SetItems(items)
}

// exportRowText is the name and the kind of one tickable row.
func (m *Model) exportRowText(key string) (name, kind string) {
	switch key {
	case exportDefaults:
		return tr(tExportRowDefaults), ""
	case exportNotes:
		return tr(tExportRowNotes), ""
	}
	for _, p := range []struct {
		prefix string
		label  textID
	}{
		{exportConnPref, tExportKindConnection}, {exportSvcPref, tExportKindService}, {exportCredPref, tExportKindCredential},
	} {
		if name, ok := strings.CutPrefix(key, p.prefix); ok {
			return m.logText(name), strings.TrimSpace(tr(p.label))
		}
	}
	return m.logText(key), ""
}

// selection turns the ticks into the core's selection.
func (e *exportState) selection() manage.ExportSelection {
	var sel manage.ExportSelection
	for _, id := range e.list.Ticked() {
		i, err := strconv.Atoi(string(id))
		if err != nil || i < 0 || i >= len(e.entries) {
			continue
		}
		key := e.entries[i]
		switch {
		case key == exportDefaults:
			sel.Defaults = true
		case key == exportNotes:
			sel.ProviderNotes = true
		case strings.HasPrefix(key, exportConnPref):
			sel.Connections = append(sel.Connections, strings.TrimPrefix(key, exportConnPref))
		case strings.HasPrefix(key, exportSvcPref):
			sel.Services = append(sel.Services, strings.TrimPrefix(key, exportSvcPref))
		case strings.HasPrefix(key, exportCredPref):
			sel.Credentials = append(sel.Credentials, strings.TrimPrefix(key, exportCredPref))
		}
	}
	return sel
}

// closeExport leaves the export, dropping everything typed into it.
func (m *Model) closeExport() {
	e := m.export
	m.export = nil
	m.screen = screenList
	if e != nil {
		e.first, e.passphrase = "", ""
		m.screen = e.from
	}
}

// cancelExport ends the export without a file.
func (m *Model) cancelExport() {
	m.closeExport()
	m.status = tr(tExportCancelled)
}

// secretsUnavailable is why an export with secrets is not possible now, or "".
func (m *Model) secretsUnavailable() string {
	required, _, err := manage.AdminRequired(m.secrets.Vault())
	switch {
	case err != nil:
		return tr(tExportStateUnreadable, m.redactor.Apply(err.Error()))
	case !required:
		return tr(tExportNoPassphrase)
	}
	return ""
}

func (m *Model) updateExport(key tea.KeyPressMsg) tea.Cmd {
	e := m.export
	if e == nil {
		m.screen = screenList
		return nil
	}
	if e.writing {
		return nil
	}
	if keyIs(key, actBack) {
		return m.exportBack()
	}
	if keyIs(key, actSave) {
		switch e.step {
		case exportSelect, exportMode, exportPassphrase, exportTarget:
			// F2 goes on like enter, which also confirms the one-line dialogs.
			key = tea.KeyPressMsg{Code: tea.KeyEnter}
		case exportResult:
			key = tea.KeyPressMsg{Code: tea.KeyEnter}
		}
	}
	switch e.step {
	case exportSelect:
		return m.updateExportSelect(key)
	case exportMode:
		return m.updateExportMode(key)
	case exportPlainConfirm:
		switch {
		case keyIs(key, actYes):
			m.clearMessages()
			m.askExportTarget()
		case keyIs(key, actNo):
			m.clearMessages()
			e.step = exportMode
		}
	case exportPassphrase, exportTarget:
		// Only F2 of the leave question reaches a one-line step here; its dialog takes every key itself.
		if keyIs(key, actEnter) {
			return m.answerTransfer(actEnter)
		}
	case exportPreview:
		switch {
		case keyIs(key, actSave):
			m.clearMessages()
			return m.requireAdmin(m.runExport)
		default:
			e.scroll(key, m.height)
		}
	case exportResult:
		switch {
		case keyIs(key, actEnter):
			path := e.target
			m.closeExport()
			m.status = tr(tExported, m.redactor.Apply(path))
		default:
			e.scroll(key, m.height)
		}
	}
	return nil
}

func (e *exportState) scroll(key tea.KeyPressMsg, height int) {
	e.view.scroll(key, halfPage(height))
}

func (m *Model) updateExportSelect(key tea.KeyPressMsg) tea.Cmd {
	e := m.export
	if e.list.Searching() {
		// A filter being typed takes every key; enter keeps it.
		e.list.Update(key)
		return nil
	}
	if !keyIs(key, actEnter) {
		e.list.Update(key)
		return nil
	}
	m.clearMessages()
	sel := e.selection()
	if !sel.Defaults && !sel.ProviderNotes && len(sel.Connections)+len(sel.Services)+len(sel.Credentials) == 0 {
		m.fail = tr(tExportTickFirst)
		return nil
	}
	m.enterExportMode()
	return nil
}

// enterExportMode shows the secrets question.
func (m *Model) enterExportMode() {
	e := m.export
	e.step = exportMode
	e.secretsReason = m.secretsUnavailable()
	e.mode = m.exportModeList(e.secretsReason != "", e.mode)
}

// exportModeList is the kit list of the mode step on its first row, or on the marked one of before. A row
// with secrets is dimmed and says it is not possible where secretsReason holds; choosing it only says why.
func (m *Model) exportModeList(blocked bool, before *kittui.List) *kittui.List {
	spec := kittui.ListSpec{Columns: []kittui.Column{{Title: columnID("MODE"), Tone: func(cell string) kittui.TextStyle {
		if strings.HasSuffix(cell, tr(tExportNotPossible)) {
			return kittui.StyleHint
		}
		return kittui.StyleText
	}}}, Keys: listKeys(""), Search: kittui.SearchNone, Wrap: true, Empty: tFormNothingConfiguredYet}
	list, err := kittui.NewList(spec, keymap, translator(false))
	var items []kittui.ListItem
	for i, row := range exportModeRows() {
		if i > 0 && blocked {
			row += tr(tExportNotPossible)
		}
		items = append(items, kittui.ListItem{ID: choiceID(i), Cells: []string{row}})
	}
	if err == nil {
		err = list.SetItems(items)
	}
	if err != nil {
		panic("the list of the export modes is invalid: " + err.Error())
	}
	if before != nil {
		if id, ok := before.Selected(); ok {
			list.Select(id)
		}
	}
	return list
}

// askExportPassphrase shows the empty masked passphrase prompt.
func (m *Model) askExportPassphrase() {
	e := m.export
	m.dropQuestion()
	e.step = exportPassphrase
	e.first, e.passphrase, e.confirming = "", "", false
}

// ticked is the number of ticked rows.
func (e *exportState) ticked() int {
	if e.list == nil {
		return 0
	}
	return len(e.list.Ticked())
}

// dirty reports whether leaving now would drop ticks or answers; a written export loses nothing.
func (e *exportState) dirty() bool {
	return e != nil && e.step != exportResult && (e.ticked() > 0 || e.step != exportSelect)
}

// exportBack is esc: one step back, and on the first step the way out, asking first once something is
// ticked. Nothing is written before F2 on the preview.
func (m *Model) exportBack() tea.Cmd {
	e := m.export
	m.clearMessages()
	switch e.step {
	case exportSelect:
		if e.list.Searching() || e.list.Query() != "" {
			// The first esc clears the filter.
			e.list.Act(actBack)
			return nil
		}
		if e.ticked() > 0 {
			m.askLeave(false)
			return nil
		}
		m.cancelExport()
	case exportMode:
		e.step = exportSelect
	case exportPassphrase:
		e.first, e.passphrase, e.confirming = "", "", false
		m.enterExportMode()
	case exportPlainConfirm:
		m.enterExportMode()
	case exportTarget:
		e.target = strings.TrimSpace(e.target)
		switch e.kind {
		case exportEncrypted:
			m.askExportPassphrase()
		case exportUnencrypted:
			e.step = exportPlainConfirm
		default:
			m.enterExportMode()
		}
	case exportPreview:
		m.askExportTarget()
	case exportResult:
		m.closeExport()
	}
	return nil
}

func (m *Model) updateExportMode(key tea.KeyPressMsg) tea.Cmd {
	e := m.export
	if !keyIs(key, actEnter) {
		e.mode.Update(key)
		return nil
	}
	m.clearMessages()
	i, ok := chosen(e.mode, len(exportModeRows()))
	if !ok {
		return nil
	}
	e.kind = exportKind(i)
	if e.kind == exportWithout {
		m.askExportTarget()
		return nil
	}
	if e.secretsReason = m.secretsUnavailable(); e.secretsReason != "" {
		m.fail = e.secretsReason
		return nil
	}
	// The secrets are read under this window's admin session, which the vault passphrase proves.
	return m.requireAdmin(func() tea.Cmd {
		if e.kind == exportUnencrypted {
			e.step = exportPlainConfirm
			return nil
		}
		m.askExportPassphrase()
		return nil
	})
}

// transferAsk is the one-line dialog the export or the import is at, if it is at one.
func (m *Model) transferAsk() (ask, bool) {
	switch {
	case m.imp != nil:
		switch m.imp.step {
		case importFile:
			return m.importFileAsk(), true
		case importPassphrase:
			return m.importPassphraseAsk(), true
		}
	case m.export != nil:
		switch m.export.step {
		case exportPassphrase:
			return m.exportPassphraseAsk(), true
		case exportTarget:
			return m.exportTargetAsk(), true
		}
	}
	return ask{}, false
}

func (m *Model) answerTransfer(act action) tea.Cmd {
	switch {
	case m.imp != nil && m.imp.step == importFile:
		return m.answerImportFile(act)
	case m.imp != nil:
		return m.answerImportPassphrase(act)
	case m.export.step == exportTarget:
		return m.answerExportTarget(act)
	}
	return m.answerExportPassphrase(act)
}

// exportPassphraseAsk is the masked dialog of the export passphrase, typed twice; F2 goes on like enter.
func (m *Model) exportPassphraseAsk() ask {
	q := ask{title: tExportPassTitle, question: tr(tExportPassHint), input: true, masked: true, keep: actEnter,
		answers: []kittui.Answer{{Action: actEnter, Label: hintLabelID("{save} or {enter} ok")},
			{Action: actSave, Label: hintLabelID("{save} or {enter} ok")},
			{Action: actBack, Label: hintLabelID("{back} back, nothing written")}}}
	if m.export.confirming {
		q.title, q.question = tExportPassConfirmTitle, tr(tVaultConfirmHint)
	}
	return q
}

// answerExportPassphrase takes the export passphrase twice; a difference asks again from the start.
func (m *Model) answerExportPassphrase(act action) tea.Cmd {
	e := m.export
	switch act {
	case actBack:
		return m.exportBack()
	case actEnter, actSave:
	default:
		return nil
	}
	typed := m.takeInput()
	m.clearMessages()
	switch {
	case !e.confirming && typed == "":
		m.fail = tr(tExportPassEmpty)
	case !e.confirming:
		e.first, e.confirming = typed, true
	case typed != e.first:
		e.first, e.confirming = "", false
		m.fail = tr(tPassphraseMismatch)
	default:
		e.passphrase, e.first, e.confirming = typed, "", false
		m.askExportTarget()
	}
	return nil
}

// askExportTarget moves to the path prompt, with the file typed before or one in the start directory.
func (m *Model) askExportTarget() {
	e := m.export
	m.dropQuestion()
	e.step = exportTarget
	if e.target == "" && m.startDir != "" {
		e.target = filepath.Join(m.startDir, exportFileName)
	}
}

// exportTargetAsk is the dialog of the file the export writes, with the files and directories that complete
// it; F2 goes on like enter.
func (m *Model) exportTargetAsk() ask {
	return ask{title: tExportFileTitle, question: tr(tExportFileHint), input: true, value: m.export.target,
		suggest: projectpath.Files, saveEnters: true, keep: actEnter,
		answers: []kittui.Answer{{Action: actEnter, Label: hintLabelID("{save} or {enter} preview")},
			{Action: actSave, Label: hintLabelID("{save} or {enter} preview")},
			{Action: actBack, Label: hintLabelID("{back} back")}}}
}

// answerExportTarget previews the export to the typed file; esc goes one step back.
func (m *Model) answerExportTarget(act action) tea.Cmd {
	e := m.export
	switch act {
	case actBack:
		return m.exportBack()
	case actEnter, actSave:
	default:
		return nil
	}
	m.clearMessages()
	path := strings.TrimSpace(e.target)
	if path == "" {
		m.fail = tr(tExportNameFile)
		return nil
	}
	path = absolutePath(path)
	// Nothing is read or written yet: the preview never holds a secret value.
	res, err := m.svc.ExportConfig(e.selection(), "")
	if err != nil {
		m.fail = m.redactor.Apply(err.Error())
		return nil
	}
	e.target = path
	e.view.reset()
	e.lines = m.exportLines(res, true)
	e.step = exportPreview
	return nil
}

// runExport writes the file once the preview is confirmed. It runs as a command: reading the vault and
// encrypting the file do scrypt work that must not block the event loop. The vault is unlocked by the admin
// check just before; no passphrase of it is kept anywhere.
func (m *Model) runExport() tea.Cmd {
	e := m.export
	if e == nil || e.writing {
		return nil
	}
	e.writing = true
	m.busy = tr(tBusyExporting)
	kind, sel, path, svc := e.kind, e.selection(), e.target, m.svc
	if kind != exportWithout {
		m.vaultBusy = true
	}
	passphrase := e.passphrase
	return func() tea.Msg {
		var (
			res *manage.ExportResult
			err error
		)
		if kind == exportWithout {
			res, err = svc.ExportConfig(sel, path)
			return exportDoneMsg{res: res, err: err, path: path}
		}
		opts := manage.SecretExport{
			VaultPassphrase: func(string) (string, error) { return "", errors.New(tr(tExportVaultLock)) },
		}
		if kind == exportUnencrypted {
			opts.Unencrypted = true
		} else {
			opts.ExportPassphrase = func(string) (string, error) { return passphrase, nil }
		}
		res, err = svc.ExportConfigWithSecrets(sel, path, opts)
		return exportDoneMsg{res: res, err: err, path: path}
	}
}

func (m *Model) handleExportDone(msg exportDoneMsg) tea.Cmd {
	m.busy, m.vaultBusy = "", false
	e := m.export
	if e == nil {
		return nil
	}
	e.writing = false
	if msg.err != nil {
		// Back at the path, where an existing file is answered by another name; the choices stay.
		m.askExportTarget()
		m.fail = m.exportError(msg.err, msg.path)
		return nil
	}
	e.first, e.passphrase = "", ""
	e.view.reset()
	e.lines = m.exportLines(msg.res, false)
	e.step = exportResult
	m.clearMessages()
	return nil
}

// exportError words a refused export; the core never replaces a file.
func (m *Model) exportError(err error, path string) string {
	if errors.Is(err, fs.ErrExist) {
		return tr(tExportExists, m.redactor.Apply(path))
	}
	return m.redactor.Apply(err.Error())
}

// exportLines are the preview or the result, one line each, in the order of 'qconnect config export'. A line
// that starts with "warning" is drawn as one. They name credentials and roles, never a value.
func (m *Model) exportLines(res *manage.ExportResult, preview bool) []string {
	e := m.export
	apply := m.redactor.Apply
	names := func(list []string) string { return apply(strings.Join(list, ", ")) }
	var lines []string
	lines = append(lines, tr(tExLineFile, apply(e.target)))
	switch {
	case e.kind == exportWithout:
		lines = append(lines, tr(tExLineSecretsNone))
	case preview && e.kind == exportEncrypted:
		lines = append(lines, tr(tExLineSecretsEncPreview))
	case preview:
		lines = append(lines, tr(tExLineWarnPlain))
	case res.Encrypted:
		lines = append(lines, tr(tExLineSecretsEnc))
	}
	for _, line := range []struct {
		label textID
		names []string
	}{
		{tExLineServices, res.Services}, {tExLineCredentials, res.Credentials}, {tExLineConnections, res.Connections},
		{tExLineDefaults, res.Defaults}, {tExLineNotes, res.ProviderNotes},
	} {
		if len(line.names) > 0 {
			lines = append(lines, tr(line.label, names(line.names)))
		}
	}
	for _, dep := range res.Dependencies {
		lines = append(lines, tr(tExLineNeeded, dep.Kind, apply(dep.Name), names(dep.For)))
	}
	if len(res.DefaultsOmitted) > 0 {
		lines = append(lines, tr(tExLineLeftOut, names(res.DefaultsOmitted)))
	}
	if len(res.SecretsNotIncluded) > 0 {
		switch {
		case preview && e.kind != exportWithout:
			lines = append(lines, tr(tExLineReads, names(res.SecretsNotIncluded)))
		case e.kind != exportWithout:
			lines = append(lines, tr(tExLineWithoutNone, names(res.SecretsNotIncluded)))
		default:
			lines = append(lines, tr(tExLineWithoutSet, names(res.SecretsNotIncluded)))
		}
	}
	for _, s := range res.SecretsIncluded {
		lines = append(lines, tr(tExLineSecret, apply(s.Credential), apply(strings.Join(s.Roles, ", "))))
	}
	if len(res.SecretsMissing) > 0 {
		lines = append(lines, tr(tExLineMissing, names(res.SecretsMissing)))
	}
	for _, w := range res.Warnings {
		lines = append(lines, tr(tExLineWarning, apply(w)))
	}
	return lines
}

func (m *Model) exportSelectFrame() (string, string) {
	p := m.dialogPanelOf()
	head := p.top(tr(tExportSelectTitle), m.export.list.Position()) + p.text(styles.hint, tr(tExportSelectHint)) + p.blank()
	return head, p.closing() + m.keyHint(m.exportKeys()) + m.notes()
}

// exportView draws the step the export is at.
func (m *Model) exportView() string {
	e := m.export
	if e == nil {
		return ""
	}
	if m.backdrop && (e.step == exportPassphrase || e.step == exportTarget) {
		// A dialog stands over the question that led to it.
		saved := e.step
		e.step = exportMode
		defer func() { e.step = saved }()
		return m.exportView()
	}
	switch e.step {
	case exportSelect:
		head, foot := m.exportSelectFrame()
		return m.workList(e.list, head, foot)
	case exportMode:
		if e.mode == nil {
			e.mode = m.exportModeList(e.secretsReason != "", nil)
		}
		p := m.dialogPanelOf()
		head := p.top(tr(tExportSecretsTitle), "") + p.blank()
		note := p.hint(tr(tExportSecretsNote))
		if e.secretsReason != "" {
			note = p.hint(e.secretsReason)
		}
		return m.workList(e.mode, head, p.blank()+note+p.bottom()+m.keyHint(keysExportMode)+m.notes())
	case exportPlainConfirm:
		p := m.dialogPanelOf()
		return p.top(tr(tExportPlainTitle), "") + p.blank() +
			p.text(styles.warning, warnMark+" "+tr(tExportPlainWarn)) + p.bottom() +
			m.keyHint(keysExportPlain) + m.notes()
	}
	title, question := tr(tExportPreviewTitle), tr(tTransferPreviewQuestion)
	if e.step == exportResult {
		title, question = tr(tExportWrittenTitle), ""
	}
	return m.linesView(title, question, m.exportKeys(), e.lines, &e.view, e.step == exportResult)
}

// exportKeys is the key text of the step the export is at.
func (m *Model) exportKeys() string {
	e := m.export
	if e == nil {
		return ""
	}
	switch {
	case e.step == exportPassphrase || e.step == exportTarget:
		// The dialog names its keys.
		return ""
	case e.step == exportSelect && e.list.Searching():
		return hintsText(e.list.Hints())
	case e.step == exportSelect:
		return keysExportSelect
	case e.step == exportMode:
		return keysExportMode
	case e.step == exportPlainConfirm:
		return keysExportPlain
	case e.writing:
		return tr(tWorkWritingWait)
	case e.step == exportResult:
		return keysTransferDone
	}
	return keysTransferPreview
}

// isWarningLine reports whether a line of a preview or result is a warning, which the label of the line says.
func isWarningLine(line string) bool {
	return strings.HasPrefix(line, strings.Fields(tr(tExLineWarning))[0])
}

// linesView draws a preview or a result: the lines scrolled to the position of view, which it clamps, in a
// panel under its title and above the question and keys of the step. A line that starts with "warning" is drawn as one,
// with its mark; a finished step opens with its state.
func (m *Model) linesView(title, question, keys string, lines []string, view *textScroll, done bool) string {
	p := m.dialogPanelOf()
	var body []string
	warned := false
	for _, line := range lines {
		if isWarningLine(line) {
			warned = true
		}
		body = append(body, m.reportLines(p, line)...)
	}
	kind, word := statusOK, tr(tStatusDone)
	if warned {
		kind, word = statusWarn, tr(tStatusDoneWarn)
	}
	headOf := func(note string) string {
		switch {
		case done && p.framed:
			return p.top(title, note) + p.result(kind, word) + p.blank()
		case done:
			return m.wrapped(styles.title, title) + "  " + statusMark(kind, word) + "\n\n"
		case p.framed:
			return p.top(title, note) + p.blank()
		}
		return p.top(title, "") + "\n"
	}
	head := headOf("")
	foot := p.closing()
	if question != "" {
		foot += m.wrapped(styles.hint, question)
	}
	foot += m.keyHint(keys) + m.notes()
	room := max(m.height-strings.Count(head, "\n")-strings.Count(foot, "\n")-1, 3)
	shown := view.window(body, room, false)
	if p.framed && len(body) > room {
		head = headOf(fmt.Sprintf("%d-%d/%d", view.offset()+1, view.offset()+len(shown), len(body)))
	}
	return head + strings.Join(shown, "\n") + "\n" + foot
}

// reportLines draws one line of a report: its label dimmed and its value plain, a warning with its mark and
// colour. A line that does not fit continues on the next row; the lead keeps the columns of the labels.
func (m *Model) reportLines(p dialogPanel, line string) []string {
	warn := isWarningLine(line)
	lead := "  "
	if warn {
		lead = styles.warning.Render(warnMark) + " "
	}
	var out []string
	for i, piece := range strings.Split(render(lipgloss.NewStyle(), max(p.room-2, 1), line), "\n") {
		text := piece
		switch {
		case warn:
			text = styles.warning.Render(piece)
		case i == 0 && !strings.HasPrefix(piece, " "):
			label, value, _ := strings.Cut(piece, " ")
			text = styles.hint.Render(label) + styles.cell.Render(" "+value)
		default:
			text = styles.cell.Render(piece)
		}
		prefix := lead
		if i > 0 {
			prefix = "  "
		}
		out = append(out, p.side(prefix+text))
	}
	return out
}

// exportLeaveAsk is the leave question over an unfinished export.
func (m *Model) exportLeaveAsk() ask {
	save := "{save} next step"
	if m.export != nil && m.export.step == exportPreview {
		save = "{save} write"
	}
	if m.export != nil && m.export.step == exportPlainConfirm {
		// F2 does not answer the plain text question; only y does.
		save = ""
	}
	title, discard, why := tr(tExportLeaveTitle), "{discard} discard", tr(tExportLeaveWhy)
	if m.leaveQuit {
		title, discard, why = tr(tExportQuitTitle), "{discard} discard and quit", tr(tTransferQuitWhy)
	}
	return m.leaveQuestion(title, why, tr(tExportLeaveWarn), tr(tTransferNothingWritten), save, discard)
}
