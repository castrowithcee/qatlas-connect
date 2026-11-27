package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// The navigation takes the form the preferences choose: the tab line with the option bar, a path line, the
// path in the header, or a choice field of the areas in the header above the option bar. The path and the
// choice field open the kit's overlays of the areas and their parts; a choice there is the click on that tab.

// pathNavKeys are the keys the overlays read, those of the lists.
var pathNavKeys = kit.PathNavKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome,
	Last: actEnd, Left: actLeft, Right: actRight, Enter: actEnter, Back: actBack}

// navForm is the frame's form of the navigation in force.
func (m *Model) navForm() kit.NavForm {
	switch m.prefs.Navigation {
	case kitprefs.NavigationPathLine:
		return kit.NavPathLine
	case kitprefs.NavigationPathHeader:
		return kit.NavPathHeader
	case kitprefs.NavigationHeaderChoice:
		return kit.NavHeaderChoice
	}
	return kit.NavTabs
}

// pathForm reports whether the navigation is a path, in its own line or in the header.
func (m *Model) pathForm() bool {
	f := m.navForm()
	return f == kit.NavPathLine || f == kit.NavPathHeader
}

// navZones are the lines of the navigation the form in force draws, top down, and the workspace below them
// as ZoneRows.
func (m *Model) navZones() []kit.FocusZone {
	if m.pathForm() {
		return []kit.FocusZone{kit.ZonePath, kit.ZoneRows}
	}
	zones := []kit.FocusZone{kit.ZoneTabs}
	if m.hasSubTabs() {
		zones = append(zones, kit.ZoneSubtabs)
	}
	return append(zones, kit.ZoneRows)
}

// navLine is the line of the navigation screenNav stands on; a line the form or the area does not draw
// leads to the top one.
func (m *Model) navLine() kit.FocusZone {
	zones := m.navZones()
	if m.navZone != kit.ZoneRows && slices.Contains(zones, m.navZone) {
		return m.navZone
	}
	return zones[0]
}

// stepNav is left or right on the line in focus: the next area or part, or on the path its other section.
func (m *Model) stepNav(step int) tea.Cmd {
	areas := m.kitAreas()
	active := m.section.area()
	switch m.navLine() {
	case kit.ZonePath:
		m.pathArea = step < 0
		return nil
	case kit.ZoneSubtabs:
		tab := kit.StepTab(areas[active], kit.TabID(m.section.key()), step)
		return m.previewSection(areaSections[active][slices.IndexFunc(areas[active].Tabs, func(t kit.Tab) bool { return t.ID == tab })])
	}
	next := kit.StepArea(areas, kit.AreaID(active.key()), step)
	a, _ := areaByName(string(next))
	return m.previewArea(a)
}

// navSpec is what the overlays read of the frame: the areas, the current place and the focus of the path. An
// area of one part has no tabs there, so the overview lists it as a place of its own instead of a group
// around its only part.
func (m *Model) navSpec() kit.FrameSpec {
	active, tab, mode := m.kitActive()
	areas := m.kitAreas()
	for i := range areas {
		if len(areas[i].Tabs) < 2 {
			areas[i].Tabs = nil
		}
	}
	return kit.FrameSpec{Areas: areas, ActiveArea: active, ActiveTab: tab, ModeTab: mode, PathFocusArea: m.pathArea}
}

// openMenu opens the overlay open chooses, built with the keys and texts in force.
func (m *Model) openMenu(open func(*kit.PathNav, kit.FrameSpec) bool) bool {
	p, err := kit.NewPathNav(pathNavKeys, keymap, translator(false))
	if err != nil || !open(p, m.navSpec()) {
		return false
	}
	m.menu, m.menuTargets = p, nil
	return true
}

// openFocusedMenu is enter on the path or the choice field: it opens their overlay.
func (m *Model) openFocusedMenu() bool {
	switch {
	case m.navLine() == kit.ZonePath:
		return m.openMenu((*kit.PathNav).OpenFocused)
	case m.navForm() == kit.NavHeaderChoice && m.navLine() == kit.ZoneTabs:
		return m.openMenu((*kit.PathNav).OpenAreas)
	}
	return false
}

// clickNavField is the click on a section of the path or on the choice field: it leaves the screen as a click
// on a tab does and then acts as enter on what was clicked.
func (m *Model) clickNavField(r kit.MouseResult) tea.Cmd {
	lead, ok := m.leaveForBar()
	if !ok {
		return lead
	}
	if m.screen != screenNav {
		m.focusNav()
	}
	m.navZone = r.Zone
	if r.Zone == kit.ZonePath {
		m.pathArea = r.Tab == ""
		m.openMenu(func(p *kit.PathNav, spec kit.FrameSpec) bool { return p.OpenClick(spec, r) })
		return lead
	}
	m.openMenu((*kit.PathNav).OpenAreas)
	return lead
}

// menuReachable reports whether the menu key opens the overview: in the forms with an overlay, on the screens
// the digits of the areas reach, where nothing is typed.
func (m *Model) menuReachable() bool {
	if m.navForm() == kit.NavTabs || m.wizard != nil || m.typing() {
		return false
	}
	switch m.screen {
	case screenNav, screenList, screenLogs:
		return true
	}
	return false
}

// updateMenu gives a key to the open overlay. The screen behind it takes none.
func (m *Model) updateMenu(key tea.KeyPressMsg) tea.Cmd {
	choice, _ := m.menu.Update(m.moveKey(key))
	return m.chooseMenu(choice)
}

// clickMenu gives a click or a notch of the wheel to the open overlay, which holds the only targets.
func (m *Model) clickMenu(msg tea.MouseMsg) tea.Cmd {
	r := kit.NewMouseMap(m.menuTargets, true).Handle(msg)
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		m.menu.Wheel(r.WheelSteps)
		return nil
	}
	choice, _ := m.menu.Click(r)
	return m.chooseMenu(choice)
}

// chooseMenu runs a choice of the overlay as the click on its tab; the zero value is no choice.
func (m *Model) chooseMenu(c kit.PathChoice) tea.Cmd {
	if !m.menu.Open() {
		m.menu, m.menuTargets = nil, nil
	}
	if c.Area == "" {
		return nil
	}
	return m.clickPlace(c.Area, c.Tab)
}

// menuView draws the open overlay over the screen, which takes no click then. Where the terminal is too
// narrow for the modal, the overlay fills the workspace and its keys the footer.
func (m *Model) menuView() string {
	m.backdrop = true
	background := m.frame(m.workspaceView())
	m.backdrop = false
	m.frameTabs = nil
	if m.tooSmall {
		return background
	}
	view, targets, ok := m.menu.Overlay(styles.Styles, background, m.termWidth, m.termHeight,
		kit.Rect{Width: m.termWidth, Height: m.termHeight})
	if ok {
		m.menuTargets = targets
		return view
	}
	spec := m.spec
	spec.Back, spec.HeaderKeys, spec.Footer, spec.Input = nil, nil, m.menu.Hints(), kit.InputNavigation
	frame := m.kitFrame()
	room := frame.Workspace(spec)
	body, bodyTargets := m.menu.View(styles.Styles, room.Width, room.Height)
	spec.Workspace = body
	view, targets = frame.Render(styles.Styles, spec)
	m.menuTargets = kit.OffsetTargets(bodyTargets, room.X, room.Y)
	for _, t := range targets {
		if t.Rect.Y >= room.Y+room.Height {
			m.menuTargets = append(m.menuTargets, t)
		}
	}
	return view
}
