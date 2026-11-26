package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A part of the kit that answers with an action, such as the help, stands as the open modal over the current
// screen: it takes every key, click and notch of the wheel first, the screen under it takes none, and what it
// answers runs through the function its opener gave.

// showModal opens part over the current screen; done runs each action part answers with.
func (m *Model) showModal(part kittui.Modal, done func(action) tea.Cmd) {
	m.modal, m.modalDone, m.modalTargets = part, done, nil
}

// questionTop is the first row a question may cover: the top line and the tab line stay in view.
const questionTop = 2

func (m *Model) closeModal() { m.modal, m.modalDone, m.modalTargets = nil, nil, nil }

// dialogExtras are the actions a dialog leaves to its opener: the hints behind its answers that open the
// command line and the help over it, and F2, which a question may leave to the screen under it.
var dialogExtras = []action{actCommand, actHelp, actSave}

// modalKey gives a key to the open modal; ctrl+c still quits.
func (m *Model) modalKey(msg tea.KeyPressMsg) tea.Cmd {
	if keyIs(msg, actForceQuit) {
		return m.interrupt()
	}
	act, handled := m.modal.Update(msg)
	if _, dialog := m.modal.(*kittui.Dialog); dialog && !handled {
		for _, a := range dialogExtras {
			if keyIs(msg, a) {
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
	act, handled := m.modal.Click(r)
	if _, dialog := m.modal.(*kittui.Dialog); dialog && !handled && slices.Contains(dialogExtras, r.Action) {
		act = r.Action
	}
	return m.modalAct(act)
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
	base := m.screen
	if m.question != nil {
		base = m.baseScreen()
	}
	var screen string
	m.renderAs(base, 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
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
