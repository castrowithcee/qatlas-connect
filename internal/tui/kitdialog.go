package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A question is a screen that keeps its state, such as the flag of screenConfirm that names it, and an ask
// that describes it. The kit's dialog built from the ask stands as the open modal over the screen beneath,
// and its answer runs through the handler of the screen.

// ask describes a question: its texts, its answers, the answer enter gives and the one that destroys.
type ask struct {
	title       textID
	question    string
	consequence string
	// irreversible draws the consequence as a warning.
	irreversible bool
	details      []kittui.DialogLine
	answers      []kittui.Answer
	// keep is the answer enter gives: one that destroys, writes and installs nothing.
	keep action
	// danger is the answer that destroys, if any; a click beside such a question answers nothing.
	danger action
	// save leaves F2 to the screen under the question, which saves from there as it does without it.
	save bool
}

// yesNo are the answers y and n, with esc answering as n does; yes and no are the key groups of the
// catalog that name them.
func yesNo(yes, no string) []kittui.Answer {
	return []kittui.Answer{{Action: actYes, Label: hintLabelID(yes)}, {Action: actNo, Label: hintLabelID(no)},
		{Action: actBack, Label: hintLabelID(no)}}
}

// dialogOf is the kit's dialog of q. Every text is redacted first, and the command line and the help stand
// behind the answers where the screen reaches them.
func (m *Model) dialogOf(q ask) (*kittui.Dialog, error) {
	var details []kittui.DialogLine
	for _, d := range q.details {
		// The dialog escapes a line break, so each line of a text is a detail of its own.
		for _, line := range strings.Split(m.redactor.Apply(d.Text), "\n") {
			d.Text = line
			details = append(details, d)
		}
	}
	var extra []kittui.Hint
	if m.helpReachable() {
		extra = []kittui.Hint{{Action: actCommand, Label: hintLabelID("{command} cmds"), Priority: footerCommand},
			{Action: actHelp, Label: hintLabelID("{help} help"), Priority: footerHelp}}
	}
	return kittui.NewDialog(kittui.DialogSpec{
		Title:        q.title,
		Question:     m.redactor.Apply(q.question),
		Consequence:  m.redactor.Apply(q.consequence),
		Details:      details,
		Irreversible: q.irreversible,
		Answers:      q.answers,
		Default:      q.keep,
		Danger:       q.danger,
		CloseOutside: actBack,
		Keys: kittui.DialogKeys{Enter: actEnter, Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown,
			First: actHome, Last: actEnd},
		Extra: extra,
	}, keymap, translator(infoExtraLabel() != "" && !m.infoReachable()))
}

// currentAsk is the question the current screen asks, and false where it asks none.
func (m *Model) currentAsk() (ask, bool) {
	switch m.screen {
	case screenConfirm:
		return m.confirmAsk()
	case screenLeave:
		return m.leaveAsk(), true
	case screenUpdate:
		return m.updateAsk(), true
	case screenRestart:
		return m.restartAsk(), true
	case screenRename:
		if m.rename != nil && m.rename.plan != nil {
			return m.renameAsk(), true
		}
	case screenPaths:
		if m.pathRemove {
			return m.pathRemoveAsk(), true
		}
	case screenTargets:
		if m.targetRemove {
			return m.targetRemoveAsk(), true
		}
	}
	return ask{}, false
}

// answerAsk runs an answer of the question the current screen asks.
func (m *Model) answerAsk(act action) tea.Cmd {
	switch m.screen {
	case screenConfirm:
		return m.answerConfirm(act)
	case screenLeave:
		return m.answerLeave(act)
	case screenUpdate:
		return m.answerUpdate(act)
	case screenRestart:
		return m.answerRestart(act)
	case screenRename:
		return m.answerRename(act)
	case screenPaths:
		return m.answerPathRemove(act)
	case screenTargets:
		return m.answerTargetRemove(act)
	}
	return nil
}

// cancelAnswers are y and the answers n and esc that keep everything, as cancel.
func cancelAnswers(yes string) []kittui.Answer { return yesNo(yes, "{no}/{back} cancel") }

// syncQuestion keeps the dialog of the question the current screen asks open: it opens it where no other
// modal stands over the screen, builds it anew when the question changed, and closes it once the screen asks
// it no more. The help keeps it under itself and shows it again when it closes.
func (m *Model) syncQuestion() {
	q, ok := m.currentAsk()
	key := ""
	if ok {
		key = fmt.Sprintf("%v", q)
	}
	if m.question != nil && key != m.questionKey {
		if m.modal == m.question {
			m.closeModal()
		}
		m.question, m.questionKey = nil, ""
	}
	if !ok || m.modal != nil {
		return
	}
	if m.question == nil {
		d, err := m.dialogOf(q)
		if err != nil {
			// A question that cannot be drawn is answered as esc answers it, so it never holds the screen.
			m.answerAsk(actBack)
			m.fail = err.Error()
			return
		}
		m.question, m.questionKey = d, key
	}
	m.showModal(m.question, m.answerQuestion)
}

// answerQuestion runs what the dialog of the question answered; the help and the command line open over it.
func (m *Model) answerQuestion(act action) tea.Cmd {
	switch act {
	case actHelp:
		m.openHelp()
		return nil
	case actCommand:
		m.closeModal()
		m.openCommandLine()
		return nil
	case actSave:
		q, _ := m.currentAsk()
		if !q.save && !slices.ContainsFunc(q.answers, func(a kittui.Answer) bool { return a.Action == actSave }) {
			return nil
		}
	}
	m.closeModal()
	m.question, m.questionKey = nil, ""
	cmd := m.answerAsk(act)
	// As after any key of a screen, the profile row shows what the ticks now are.
	if m.screen == screenForm {
		m.syncProfile()
	}
	return cmd
}
