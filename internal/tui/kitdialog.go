package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/projectpath"
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
	// input gives the dialog an input line, which masked hides; the answers read it with takeInput. The dialog
	// stays open over its answer and closes once the screen asks it no more.
	input, masked bool
	// value is the text an unmasked input starts with; keepTyped hands every change of it back.
	value string
	// suggest offers the paths of this mode under the input, none where it is zero.
	suggest projectpath.Mode
	// saveEnters makes F2 go on as enter does, a marked suggestion included.
	saveEnters bool
	// notice makes the dialog a calm message, which a click beside it closes as enter does.
	notice bool
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
	// Every key but the answers waits behind a notice.
	if m.helpReachable() && !q.notice {
		extra = []kittui.Hint{{Action: actCommand, Label: hintLabelID("{command} cmds"), Priority: footerCommand},
			{Action: actHelp, Label: hintLabelID("{help} help"), Priority: footerHelp}}
	}
	keys := kittui.DialogKeys{Enter: actEnter, Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown,
		First: actHome, Last: actEnd}
	var input *kittui.DialogInput
	if q.input {
		input = &kittui.DialogInput{Masked: q.masked, Value: q.value}
	}
	if q.suggest != 0 && input != nil {
		input.Suggest = m.pathSuggestions(q.suggest)
		keys.Complete, keys.SuggestUp, keys.SuggestDown = actNext, actSuggestPrev, actSuggestNext
	}
	outside := actBack
	if q.notice {
		outside = q.keep
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
		Notice:       q.notice,
		CloseOutside: outside,
		Input:        input,
		Keys:         keys,
		Extra:        extra,
	}, keymap, translator(infoExtraLabel() != "" && !m.infoReachable()))
}

// pathSuggestions are the paths of mode that complete a typed text, from the directory the editor was started
// in; a directory keeps a dialog that asks for a file open. The directories are read once per dialog.
func (m *Model) pathSuggestions(mode projectpath.Mode) func(string) []kittui.Suggestion {
	paths := projectpath.NewSuggester(mode, m.startDir, "")
	return func(typed string) []kittui.Suggestion {
		var out []kittui.Suggestion
		for _, p := range paths.Suggest(typed) {
			out = append(out, kittui.Suggestion{Text: p, Continues: mode == projectpath.Files && endsWithSeparator(p)})
		}
		return out
	}
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
		switch {
		case m.pathEdit >= 0:
			return m.pathAsk(), true
		case m.pathRemove:
			return m.pathRemoveAsk(), true
		}
	case screenTargets:
		if m.targetRemove {
			return m.targetRemoveAsk(), true
		}
	case screenList, screenSummary:
		if m.testResultShown() {
			return m.testAsk(), true
		}
	case screenSecret:
		return m.secretAsk(), true
	case screenVaultOffer:
		if m.vaultOffer != nil {
			return m.vaultOfferAsk(), true
		}
	case screenAdminAuth:
		if m.adminAuth != nil {
			return m.adminAuthAsk(), true
		}
	case screenVaultUnlock:
		if m.vaultUnlock != nil {
			return m.vaultUnlockAsk(), true
		}
	case screenTransfer:
		return m.transferAsk()
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
		if m.pathEdit >= 0 {
			return m.answerPath(act)
		}
		return m.answerPathRemove(act)
	case screenTargets:
		return m.answerTargetRemove(act)
	case screenList, screenSummary:
		return m.answerTest(act)
	case screenSecret:
		return m.answerSecret(act)
	case screenVaultOffer:
		return m.answerVaultOffer(act)
	case screenAdminAuth:
		return m.answerAdminAuth(act)
	case screenVaultUnlock:
		return m.answerVaultUnlock(act)
	case screenTransfer:
		return m.answerTransfer(act)
	}
	return nil
}

// cancelAnswers are y and the answers n and esc that keep everything, as cancel.
func cancelAnswers(yes string) []kittui.Answer { return yesNo(yes, "{no}/{back} cancel") }

// syncQuestion keeps the dialog of the question the current screen asks open: it opens it where no other
// modal stands over the screen, builds it anew when the question changed, and closes it once the screen asks
// it no more. The help keeps it under itself and shows it again when it closes. A dialog with an input shows
// what runs and the last error inside itself.
func (m *Model) syncQuestion() {
	q, ok := m.currentAsk()
	key := ""
	if ok {
		// The text an input starts with follows what is typed (see keepTyped), so it builds no dialog anew.
		shape := q
		shape.value = ""
		key = fmt.Sprintf("%v", shape)
	}
	if m.question != nil && key != m.questionKey {
		m.dropQuestion()
	}
	if !ok {
		return
	}
	if m.question == nil {
		if m.modal != nil {
			return
		}
		d, err := m.dialogOf(q)
		if err != nil {
			// A question that cannot be drawn is answered as esc answers it, so it never holds the screen.
			m.answerAsk(actBack)
			m.fail = err.Error()
			return
		}
		m.question, m.questionKey = d, key
	}
	if q.input {
		m.question.SetMessage(m.inputNote())
	}
	if m.modal == nil {
		m.showModal(m.question, m.answerQuestion)
	}
}

// dropQuestion closes the dialog of the question and empties its input, so nothing typed outlives it and a
// question asked again starts empty.
func (m *Model) dropQuestion() {
	if m.question == nil {
		return
	}
	m.question.ClearInput()
	if m.modal == m.question {
		m.closeModal()
	} else {
		m.dropModal(m.question)
	}
	m.question, m.questionKey = nil, ""
}

// takeInput is the text of the input of the open question, which it empties: the value lives on only in what
// its answer hands it to.
func (m *Model) takeInput() string {
	if m.question == nil {
		return ""
	}
	typed := m.question.Value()
	m.question.ClearInput()
	return typed
}

// keepTyped hands the text of an unmasked input to the state of its screen, so a question asked over it, the
// check for unsaved input and a dialog built anew start from what is typed. A masked input never leaves its
// dialog.
func (m *Model) keepTyped() {
	q, ok := m.currentAsk()
	if !ok || !q.input || q.masked || m.question == nil {
		return
	}
	typed := m.question.Value()
	switch {
	case m.screen == screenPaths:
		m.pathValue = typed
	case m.screen == screenTransfer && m.imp != nil:
		m.imp.path = typed
	case m.screen == screenTransfer && m.export != nil:
		m.export.target = typed
	}
}

// inputNote is what an input dialog shows inside itself: the last error, else what still runs.
func (m *Model) inputNote() *kittui.Message {
	switch {
	case m.fail != "":
		return &kittui.Message{Kind: kittui.MessageError, Text: tr(tNoteError, m.redactor.Apply(m.fail))}
	case m.busy != "":
		return &kittui.Message{Kind: kittui.MessageBusy, Text: m.busy + " …"}
	}
	return nil
}

// answerQuestion runs what the dialog of the question answered; the help and the command line open over it
// where the screen reaches them.
func (m *Model) answerQuestion(act action) tea.Cmd {
	q, _ := m.currentAsk()
	switch act {
	case actHelp, actCommand:
		if !m.helpReachable() || q.notice {
			return nil
		}
		if act == actHelp {
			m.openHelp()
			return nil
		}
		// The command line stands over the question, which comes back when it closes.
		m.openCommandLine()
		return nil
	case actSave:
		if !q.save && !slices.ContainsFunc(q.answers, func(a kittui.Answer) bool { return a.Action == actSave }) {
			return nil
		}
	}
	if !q.input {
		m.closeModal()
		m.question, m.questionKey = nil, ""
	}
	cmd := m.answerAsk(act)
	// As after any key of a screen, the profile row shows what the ticks now are.
	if m.screen == screenForm {
		m.syncProfile()
	}
	return cmd
}
