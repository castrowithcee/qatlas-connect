package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A part of the kit that answers with an action, such as the help, stands as the open modal over the current
// screen: it takes every key, click and notch of the wheel first, the screen under it takes none, and what it
// answers runs through the function its opener gave.

// showModal opens part over the current screen, or over the open modal, which comes back when part closes;
// done runs each action part answers with.
func (m *Model) showModal(part kittui.Modal, done func(action) tea.Cmd) {
	if m.modal != nil {
		m.modalsUnder = append(m.modalsUnder, openModal{m.modal, m.modalDone})
	}
	m.modal, m.modalDone, m.modalTargets = part, done, nil
}

// questionTop is the first row a question may cover: the top line and the tab line stay in view.
const questionTop = 2

// closeModal closes the open modal and brings back the one it was opened over, if any.
func (m *Model) closeModal() {
	m.modal, m.modalDone, m.modalTargets = nil, nil, nil
	if n := len(m.modalsUnder); n > 0 {
		under := m.modalsUnder[n-1]
		m.modal, m.modalDone, m.modalsUnder = under.part, under.done, m.modalsUnder[:n-1]
	}
}

// dropModal takes part out of the modals under the open one, so it does not come back.
func (m *Model) dropModal(part kittui.Modal) {
	m.modalsUnder = slices.DeleteFunc(m.modalsUnder, func(o openModal) bool { return o.part == part })
}

// openModal is a modal with the function that runs its answers.
type openModal struct {
	part kittui.Modal
	done func(action) tea.Cmd
}

// dialogExtras are the actions a dialog leaves to its opener: the hints behind its answers that open the
// command line and the help over it, and F2, which a question may leave to the screen under it.
var dialogExtras = []action{actCommand, actHelp, actSave}

// modalKey gives a key to the open modal, and a paste whole to a modal that takes one; ctrl+c still quits.
// Editing the input of a dialog drops the last error, as the dialog drops its own.
func (m *Model) modalKey(msg tea.KeyPressMsg) tea.Cmd {
	if keyIs(msg, actForceQuit) {
		return m.interrupt()
	}
	if q, ok := m.currentAsk(); ok && q.saveEnters && m.modal == m.question && keyIs(msg, actSave) {
		if enter, ok := keyOf(actEnter); ok {
			msg = enter
		}
	}
	typed := m.modalText()
	var act action
	handled := true
	if content, ok := pasted(msg); ok {
		if p, ok := m.modal.(interface{ Paste(string) bool }); ok {
			p.Paste(content)
		}
	} else {
		act, handled = m.modal.Update(msg)
	}
	m.edited(typed)
	if _, dialog := m.modal.(*kittui.Dialog); dialog && !handled {
		for _, a := range dialogExtras {
			// The second way of the help opens it over a question as its first way does.
			if keyIs(msg, a) || a == actHelp && keyIs(msg, actHelpF9) && !m.askingInput() {
				act = a
			}
		}
	}
	return m.modalAct(act)
}

// modalMouse gives a click or a notch of the wheel to the open modal, whose targets are the only ones.
func (m *Model) modalMouse(msg tea.MouseMsg) tea.Cmd {
	r := kittui.NewMouseMap(m.modalTargets, true).Handle(msg)
	if !r.Handled {
		return nil
	}
	m.touchAdminSessionIfActive()
	if r.WheelSteps != 0 {
		// A dialog scrolls only under a notch over its details.
		if _, dialog := m.modal.(*kittui.Dialog); !dialog || r.Zone == kittui.ZoneDetail {
			m.modal.Wheel(r.WheelSteps)
		}
		return nil
	}
	if f, form := m.modal.(*kittui.Form); form && r.Item == "" && r.Action != "" {
		// A form leaves the keys of its key line and of the footer to the application, as the kit's log view does.
		act, _ := f.Act(r.Action)
		return m.modalAct(act)
	}
	typed := m.modalText()
	act, handled := m.modal.Click(r)
	m.edited(typed)
	if _, dialog := m.modal.(*kittui.Dialog); dialog && !handled && slices.Contains(dialogExtras, r.Action) {
		act = r.Action
	}
	return m.modalAct(act)
}

// modalText is the text of the input of the open dialog, "" for any other modal.
func (m *Model) modalText() string {
	if d, ok := m.modal.(*kittui.Dialog); ok {
		return d.Value()
	}
	return ""
}

// edited drops the last error once the input of the open dialog no longer holds typed, and keeps what an
// unmasked input holds now.
func (m *Model) edited(typed string) {
	if m.modalText() != typed {
		m.fail = ""
		m.keepTyped()
	}
}

func (m *Model) modalAct(act action) tea.Cmd {
	if act == "" || m.modalDone == nil {
		return nil
	}
	return m.modalDone(act)
}

// modalFrame draws the open modal over the dimmed current screen, or the screen a question stands over, where
// it fits, else in the workspace with its keys in the footer, and keeps its targets: in the second case those
// of the footer too, while header and tabs take no click.
func (m *Model) modalFrame() string {
	base := m.underScreen()
	if m.question != nil {
		base = m.baseScreen()
	}
	if base == screenForm && m.fields == nil {
		// A dialog over a form that is gone stands over an empty workspace.
		base = m.screen
	}
	var screen string
	m.renderAs(base, 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		if d, ok := m.modal.(*kittui.Dialog); ok && d.Input() == kittui.InputText {
			// A dialog with an input shows what runs and the last error itself.
			fail, busy := m.fail, m.busy
			m.fail, m.busy = "", ""
			defer func() { m.fail, m.busy = fail, busy }()
		}
		screen = m.frame(m.workspaceView())
	})
	// A screen under the modal too large for the terminal leaves the modal its clicks.
	m.tooSmall = false
	area := kittui.Rect{Width: m.termWidth, Height: m.termHeight}
	if _, question := m.modal.(*kittui.Dialog); question {
		// A question leaves the top line and the tab line in view, so it never hides where it was asked.
		area.Y, area.Height = questionTop, max(m.termHeight-questionTop, 0)
	}
	view, targets, ok := m.modal.Overlay(styles.Styles, screen, m.termWidth, m.termHeight, area)
	if !ok {
		spec := m.spec
		spec.Footer, spec.Input, spec.Focus = m.modal.Hints(), kittui.InputNavigation, ""
		// A modal that reads text names only the keys that act there.
		if t, ok := m.modal.(interface{ Input() kittui.InputFocus }); ok {
			spec.Input = t.Input()
		}
		spec.HeaderKeys, spec.Back, spec.Position = nil, nil, ""
		frame := m.kitFrame()
		room := frame.Workspace(spec)
		var body []kittui.Target
		spec.Workspace, body = m.modal.View(styles.Styles, room.Width, room.Height)
		var all []kittui.Target
		view, all = frame.Render(styles.Styles, spec)
		targets = kittui.OffsetTargets(body, room.X, room.Y)
		for _, t := range all {
			if t.Rect.Y >= room.Y+room.Height {
				targets = append(targets, t)
			}
		}
	}
	m.modalTargets = targets
	return view
}
