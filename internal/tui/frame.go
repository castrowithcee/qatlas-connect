package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// Every screen stands in the kit's frame: the header, the tab line, the sub-tabs of the active area, a rule,
// the workspace and the footer. frameSpec is what the frame draws around the workspace; the workspace takes
// the room the frame leaves, and the frame's click targets go into the mouse map.

// kitFrame is the frame of the key table and the texts in force.
func (m *Model) kitFrame() *kit.Frame {
	return kit.NewFrame(keymap, translator(infoExtraLabel() != "" && !m.infoReachable()))
}

// frameSpec is the frame of the current screen without its workspace. Its footer is the key line the screen
// knows before it draws.
func (m *Model) frameSpec() kit.FrameSpec {
	active, tab, mode := m.kitActive()
	spec := kit.FrameSpec{
		Width: max(m.termWidth, 1), Height: max(m.termHeight, 1),
		Product: tr(tHeaderTitle), ProductShort: tr(tHeaderTitleShort), Icon: m.icon(iconLogo),
		Status:     append(m.kitMarks(), m.bannerMarks()...),
		HeaderKeys: m.headerKeys(),
		Areas:      m.kitAreas(), ActiveArea: active, ActiveTab: tab, ModeTab: mode,
		Nav: m.navForm(), PathFocusArea: m.pathArea,
		Focus: m.navFocus(), Input: m.inputFocus(),
		Footer:   m.keyHints(m.screenKeys),
		Position: m.screenPos,
	}
	if m.backShown() {
		spec.Back = &kit.Hint{Action: actBack, Label: tHeaderBack}
	}
	return spec
}

// navFocus is the line of the navigation that has the focus, if one has.
func (m *Model) navFocus() kit.FocusZone {
	if m.screen != screenNav {
		return ""
	}
	return m.navLine()
}

// inputFocus is the input focus of the current screen: text where it reads typed text, so the hints name
// only the keys that act there.
func (m *Model) inputFocus() kit.InputFocus {
	switch {
	case m.keyCapture:
		return kit.InputKeyCapture
	case m.typing():
		return kit.InputText
	}
	return kit.InputNavigation
}

// headerKeys are the help and command keys at the right end of the header. A screen that reads text acts on
// them first while its line is empty, so they stand there too.
func (m *Model) headerKeys() []kit.Hint {
	typed := m.helpReachable()
	return []kit.Hint{
		{Action: actHelp, Label: tHeaderHelp, WhileTyping: typed},
		{Action: actCommand, Label: tHeaderCmd, WhileTyping: typed},
	}
}

// bannerMarks is the banner of a newer release as a mark of the header, in its forms longest first. It takes
// its room before the keys at the right end, as the warnings of the vault do.
func (m *Model) bannerMarks() []kit.StatusMark {
	banners := m.updateBanners()
	if len(banners) == 0 {
		return nil
	}
	return []kit.StatusMark{{Kind: kit.StatusOK, Text: banners[0], Short: banners[1:], Rank: kit.MarkBeforeKeys, NoGlyph: true}}
}

// layoutWorkspace derives the room of the workspace from the terminal and the frame. The footer takes a
// second row where the key line does not fit one, and the key line of some screens depends on the room (a
// detail longer than it gains the scroll keys), so the room is derived once more after the key line.
func (m *Model) layoutWorkspace() {
	m.width = max(m.termWidth, 1)
	m.screenKeys, m.screenPos = "", ""
	frame := m.kitFrame()
	spec := m.frameSpec()
	m.height = max(frame.Workspace(spec).Height, 1)
	for range 2 {
		m.screenKeys, m.screenPos = m.screenKeyText(), m.screenPosition()
		spec = m.frameSpec()
		height := max(frame.Workspace(spec).Height, 1)
		if height == m.height {
			break
		}
		m.height = height
	}
	m.spec, m.top = spec, frame.Workspace(spec).Y
}

// workspaceTop is the screen row of the first line of the workspace.
func (m *Model) workspaceTop() int { return m.top }

// frame draws the workspace in the frame of the current screen and keeps the frame's click targets. Behind a
// modal the frame names no keys and takes no click but on its tabs, whose click asks leaveForBar first.
func (m *Model) frame(workspace string) string {
	spec := m.spec
	if m.backdrop {
		// The screen behind the modal keeps the focus it had.
		spec.Back, spec.Footer, spec.Focus = nil, m.keyHints(""), m.navFocus()
	}
	spec.Workspace = workspace
	view, targets := m.kitFrame().Render(styles.Styles, spec)
	if m.tooSmall {
		return view
	}
	for _, t := range targets {
		switch {
		case t.Area != "":
			m.frameTabs = append(m.frameTabs, t)
		case !m.backdrop:
			m.frameKeys = append(m.frameKeys, t)
		}
	}
	return view
}

// clickFrame is the click on a target of the frame: a tab is the tab click, a section of the path or the
// choice field opens its overlay, a key the press of its first key.
func (m *Model) clickFrame(r kit.MouseResult) tea.Cmd {
	switch {
	case r.Action != "":
		return m.pressAction(r.Action)
	case r.Zone == kit.ZonePath || r.Zone == kit.ZoneTabs && m.navForm() == kit.NavHeaderChoice:
		return m.clickNavField(r)
	}
	return m.clickPlace(r.Area, r.Tab)
}

// clickPlace is the click on the tab of an area, or of one of its parts where tab names one.
func (m *Model) clickPlace(id kit.AreaID, tab kit.TabID) tea.Cmd {
	for a := area(0); a < areaCount; a++ {
		if kit.AreaID(a.key()) != id {
			continue
		}
		if tab == "" || len(areaSections[a]) < 2 {
			return m.clickArea(a)
		}
		if i := slices.IndexFunc(areaSections[a], func(s section) bool { return kit.TabID(s.key()) == tab }); i >= 0 {
			return m.clickSubTab(areaSections[a][i])
		}
	}
	return nil
}

// clickTab is the click on a tab of the tab line or the sub-tab line, if the event is one. A dialog covers the
// tabs under it.
func (m *Model) clickTab(msg tea.MouseMsg) (tea.Cmd, bool) {
	targets, acts := m.mouseTargets(true)
	r := kit.NewMouseMap(targets, true).Handle(msg)
	if !r.Handled || r.Area == "" {
		return nil, false
	}
	return m.runTarget(r, acts), true
}

// pressAction is the press of the first key bound to an action, as a click on the hint of the action makes
// it. The hint names the action only where its key acts in the input focus of the screen, so the key goes to
// the screen as typed.
func (m *Model) pressAction(a action) tea.Cmd {
	keys := keymap.Keys(a)
	if len(keys) == 0 {
		return nil
	}
	key, ok := namedKey(keys[0])
	if !ok {
		return nil
	}
	_, cmd := m.Update(key)
	return cmd
}
