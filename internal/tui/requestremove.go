// The part of the request detail that belongs to a request to remove tools: the one confirmation before
// accepting, with what the core's preview says the connection offers afterwards.
package tui

import "github.com/castrowithcee/qatlas-connect/internal/manage"

// openRemoveChoice opens the confirmation of accepting a removal. A connection by effect has one way,
// the switch to a tools list; one with a list has none to choose, the mode stays empty.
func (m *Model) openRemoveChoice(sel manage.RequestSelection, preview manage.RequestPreview) {
	mode := manage.AcceptMode("")
	if preview.ByEffects {
		mode = manage.AcceptSwitchToList
	}
	m.requestChoice = &requestChoice{selection: sel, preview: preview, modes: []manage.AcceptMode{mode}}
	m.clearMessages()
}

// requestRemoveChoiceView is the confirmation: the one way, what it leaves, and the notes of the preview.
func (m *Model) requestRemoveChoiceView(r manage.OpenRequest) string {
	c := m.requestChoice
	note, option, follow := tr(tRequestRemoveListNote), tr(tRequestRemoveOpt), tr(tRequestRemoveFollow, c.preview.Remaining)
	if c.preview.ByEffects {
		note, option, follow = tr(tRequestRemoveEffectNote), tr(tRequestRemoveOptSwitch), tr(tRequestRemoveFollowSwitch, c.preview.Remaining)
	}
	details := []string{m.indented("> (" + radioMark(true) + ") " + option), m.indentedWith(styles.hint, "      "+follow)}
	for _, prefix := range c.preview.SplitPrefixes {
		details = append(details, m.indentedWith(styles.warning, tr(tRequestRemoveSplit, m.logText(prefix))))
	}
	if c.preview.EmptyList {
		details = append(details, m.indentedWith(styles.warning, tr(tRequestRemoveEmpty)))
	}
	if c.preview.ApprovalExpected {
		details = append(details, m.indentedWith(styles.hint, tr(tRequestApprovalExpected)))
	}
	return m.questionText(question{
		title:       tr(tRequestRemoveTitle, m.logText(r.Connection)),
		consequence: note,
		details:     details,
		keys:        keysRequestChoice,
	})
}
