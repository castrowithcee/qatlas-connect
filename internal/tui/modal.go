package tui

import (
	"strings"
)

// screenBeneath is the screen a dialog stands over, and false for a screen that is none.
func (m *Model) screenBeneath(s screen) (screen, bool) {
	switch s {
	case screenSecret:
		return screenForm, true
	case screenVaultOffer:
		switch {
		case m.imp != nil:
			return screenTransfer, true
		case m.wizard != nil && m.wizard.step == stepSummary:
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
		if m.imp != nil && (m.imp.step == importFile || m.imp.step == importPassphrase) {
			return m.imp.from, true
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
	case screenProviders:
		return screenForm, true
	case screenTemplate:
		return m.templateFromScreen(), true
	case screenVersions:
		if m.versions != nil {
			return m.versions.from, true
		}
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

// renderAs draws with the given screen and, when width is positive, workspace width, and then restores
// them together with every offset drawing clamps, so a view drawn only as a background changes no state.
func (m *Model) renderAs(s screen, width int, render func()) {
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
	}()
	m.screen = s
	if width > 0 {
		m.width = width
	}
	render()
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
