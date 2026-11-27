package tui

import (
	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-connect/internal/helptopics"
)

// openHelp opens the help topics over the current screen. The first topic ends with the keys of that screen
// and those of the global actions, as they are bound now.
func (m *Model) openHelp() {
	screen := m.keyHints(m.screenKeys)
	if m.question != nil {
		// Over a question the keys of the screen are its answers.
		screen = m.question.Hints()
	}
	h, err := kittui.NewHelp(kittui.HelpSpec{
		Topics: helptopics.All(),
		Screen: screen,
		Global: []action{actSave, actHelp, actCommand, actLock, actInfo},
		Keys: kittui.HelpKeys{Left: actLeft, Right: actRight, Next: actTopicNext, Prev: actTopicPrev,
			Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome, Last: actEnd,
			Back: actBack, Help: actHelp},
		Note:         kittui.TextID(tMacFnNote),
		CloseOutside: true,
	}, keymap, translator(infoExtraLabel() != "" && !m.infoReachable()))
	if err != nil {
		m.fail = err.Error()
		return
	}
	if m.helpTopic != "" {
		h.SetTopic(m.helpTopic)
	}
	m.clearMessages()
	m.showModal(vimHelp{h}, func(act action) tea.Cmd {
		m.helpTopic = h.Topic()
		m.closeModal()
		if act == actBack || act == actHelp {
			return nil
		}
		// A line of the key list runs as its key does on the screen.
		return m.pressAction(act)
	})
}

// vimHelp is the kit's help that also takes the vim letters for the arrows, as every screen without a text
// line does.
type vimHelp struct{ *kittui.Help }

func (h vimHelp) Update(msg tea.KeyPressMsg) (action, bool) { return h.Help.Update(vimArrow(msg)) }
