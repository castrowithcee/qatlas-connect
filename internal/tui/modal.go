package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// modalMinWidth and modalMinHeight are the smallest terminal that carries a modal over its background;
	// below them a question takes the whole workspace as before.
	modalMinWidth  = 60
	modalMinHeight = 20
	// modalMaxInner is the widest text of a modal.
	modalMaxInner = 60
)

// screenBeneath is the screen a dialog stands over, and false for a screen that is none.
func (m *Model) screenBeneath(s screen) (screen, bool) {
	switch s {
	case screenSecret:
		return screenForm, true
	case screenVaultOffer:
		if m.wizard != nil && m.wizard.step == stepSummary {
			return screenSummary, true
		}
		return screenForm, true
	case screenAdminAuth:
		if m.adminAuth != nil {
			return m.adminAuth.back, true
		}
	case screenVaultUnlock:
		if m.vaultUnlock != nil {
			return m.vaultUnlock.back, true
		}
	case screenTransfer:
		// The import's dialogs stand over the screen it was started from; the export's over its own
		// question (see exportView).
		if _, ok := m.importPrompt(); ok {
			return m.imp.from, true
		}
	case screenCommand:
		if m.cmdline != nil {
			return m.cmdline.from, true
		}
	case screenLeave:
		return m.leaveFrom, true
	case screenPicker:
		if m.valuePick != nil {
			// The value picker waits for the leave question over the form.
			return screenForm, true
		}
	case screenUpdate:
		return m.updateFrom, true
	case screenRestart:
		return m.restartBack, true
	case screenRename:
		if m.rename != nil && m.rename.back == screenFindings {
			return screenFindings, true
		}
		return screenList, true
	case screenConfirm:
		switch {
		case m.tokenRevoke != "" && m.tokenDetail != "":
			// The revoke question stands over the detail of its token.
			return screenConfirm, true
		case m.settingsTakeover != nil:
			return m.settingsTakeoverBack, true
		case m.vaultLockConfirm:
			return m.vaultLockBack, true
		case m.decryptConfirm, m.migratePlanConfirm, m.migrateDeleteConfirm, m.confirmRole != "",
			m.pendingProfile != "":
			return screenForm, true
		}
		return screenList, true
	}
	return s, false
}

// baseScreen is the screen the current dialog stands over, however many dialogs are stacked; it is the
// current screen itself when that is no dialog.
func (m *Model) baseScreen() screen {
	s := m.screen
	for range 4 {
		below, ok := m.screenBeneath(s)
		if !ok || below == s {
			return s
		}
		s = below
	}
	return s
}

// scrollViews are the scroll areas that drawing a view clamps as a side effect.
func (m *Model) scrollViews() []*textScroll {
	views := []*textScroll{&m.approval, &m.detailPane.view, &m.infoPane.view}
	if m.logs != nil {
		views = append(views, &m.logs.view)
	}
	if e := m.export; e != nil {
		views = append(views, &e.view)
	}
	if e := m.imp; e != nil {
		views = append(views, &e.view)
	}
	if st := m.st; st != nil {
		views = append(views, &st.view)
	}
	return views
}

// scrollRefs are the offsets that drawing a view clamps as a side effect.
func (m *Model) scrollRefs() []*int {
	var refs []*int
	if e := m.export; e != nil {
		refs = append(refs, &e.rows.offset)
	}
	return refs
}

// renderAs draws with the given screen and, when width is positive, workspace width, and then restores
// them together with every offset drawing clamps, so a view drawn only as a background changes no state.
func (m *Model) renderAs(s screen, width int, render func()) {
	refs := m.scrollRefs()
	saved := make([]int, len(refs))
	for i, ref := range refs {
		saved[i] = *ref
	}
	views := m.scrollViews()
	savedViews := make([]textScroll, len(views))
	for i, v := range views {
		savedViews[i] = *v
	}
	screen, w := m.screen, m.width
	defer func() {
		m.screen, m.width = screen, w
		for i, v := range views {
			*v = savedViews[i]
		}
		for i, ref := range refs {
			*ref = saved[i]
		}
	}()
	m.screen = s
	if width > 0 {
		m.width = width
	}
	render()
}

// modalView is the current question centred over its dimmed background, or false when the terminal is too
// small for that and the question takes the whole workspace.
func (m *Model) modalView() (string, bool) {
	if m.termWidth < modalMinWidth || m.termHeight < modalMinHeight {
		return "", false
	}
	inner := min(modalMaxInner, m.termWidth-8)
	var text string
	var footers []footerLine
	m.footerLog, m.inModal = &footers, true
	defer func() { m.footerLog, m.inModal = nil, false }()
	var palette []string
	var paletteIndex []int
	title := ""
	if m.screen == screenCommand {
		inner = min(modalMaxInner-4, m.termWidth-8)
		title = tr(tCmdHead)
		m.renderAs(m.screen, inner, func() { palette, paletteIndex = m.paletteLines(inner, m.termHeight-4) })
		text = strings.Join(palette, "\n")
	} else {
		ok := false
		m.renderAs(m.screen, inner, func() {
			var d promptDialog
			if d, ok = m.prompt(); ok {
				text = m.wrapped(styles.title, d.title) + "\n\n" + m.promptText(d, false) + m.promptNotes()
			}
		})
		if !ok {
			return "", false
		}
	}
	m.footerLog, m.inModal = nil, false
	base := m.baseScreen()
	if base == screenForm && m.fields == nil {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > m.termHeight-4 {
		return "", false
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, inner, "")
	}
	dialog := panel(title, lines, inner, len(lines), panelOpts{focused: true})

	var background string
	m.renderAs(base, 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	top, left := dialogOrigin(dialog, m.termWidth, m.termHeight)
	m.markDialog(dialog, top, left)
	m.hitDialog(lines, footers, top, left)
	m.hitPathRows(lines, top+1, left+2)
	for i, match := range paletteIndex {
		if match >= 0 {
			m.addHit(left+1, top+1+i, left+inner+3, top+2+i, func() tea.Cmd { return m.pickCommand(match) })
		}
	}
	return overlay(background, dialog, m.termWidth, m.termHeight), true
}

// overlay draws dialog centred over background in a screen of width by height. The background is dimmed:
// stripped of its colours and drawn faint, so it stays readable and only the dialog stands out, and still
// does without colour, where its border marks the dialog.
func overlay(background string, dialog []string, width, height int) string {
	rows := strings.Split(background, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	rows = rows[:height]
	dialogWidth := 0
	for _, line := range dialog {
		dialogWidth = max(dialogWidth, ansi.StringWidth(line))
	}
	top, left := dialogOrigin(dialog, width, height)
	for i, row := range rows {
		plain := ansi.Strip(row)
		plain += strings.Repeat(" ", max(width-ansi.StringWidth(plain), 0))
		if i < top || i >= top+len(dialog) {
			rows[i] = dim(ansi.Cut(plain, 0, width))
			continue
		}
		rows[i] = dim(ansi.Cut(plain, 0, left)) + dialog[i-top] + dim(ansi.Cut(plain, left+dialogWidth, width))
	}
	return strings.Join(rows, "\n")
}

func dim(text string) string {
	if text == "" {
		return ""
	}
	return styles.hint.Render(text)
}

// question is the shape of a question a detail screen asks in its own text: the question itself, one sentence
// on what follows from it, the detail lines the answer needs, and the key line as the last line. The questions
// that stand over a screen are the kit's dialogs; see kitdialog.go.
type question struct {
	title       string
	consequence string
	// details are finished lines (they may span several rows) between the consequence and the keys.
	details []string
	keys    string
}

func (m *Model) questionText(q question) string {
	var b strings.Builder
	b.WriteString(m.wrapped(styles.title, q.title) + "\n\n")
	b.WriteString(m.indentedWith(styles.hint, q.consequence) + "\n")
	for _, d := range q.details {
		b.WriteString(strings.TrimRight(d, "\n") + "\n")
	}
	b.WriteString(m.keyHint(q.keys))
	return b.String()
}
