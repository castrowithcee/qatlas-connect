package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// An area is a task the person came to do. It is the top level of the tab line; the section stays the
// object model beneath it, so every list, form and save keeps working on its own section. An area with
// several sections shows them as sub-tabs in a tab row below the tab line.
type area int

const (
	areaOverview area = iota
	areaLibrary
	areaConnections
	areaActivity
	areaAccess
	areaSettings
	areaCount
)

// key is the English name of the area: the name the start view is kept under and the commands are named
// after, whatever language the editor draws in.
func (a area) key() string {
	return [...]string{"Overview", "Library", "Connections", "Activity", "Access", "Settings"}[a]
}

// AreaNames returns the English names of the areas in the order of the tab line; the digit that opens an
// area is its position plus one.
func AreaNames() []string {
	names := make([]string, 0, areaCount)
	for a := area(0); a < areaCount; a++ {
		names = append(names, a.key())
	}
	return names
}

// title is the name of the area as drawn.
func (a area) title() string { return tr(areaTitles[a]) }

// areaTitles are the catalog texts of the area names, in the order of the areas.
var areaTitles = [areaCount]textID{tAreaOverview, tAreaLibrary, tAreaConnections, tAreaActivity, tAreaAccess, tAreaSettings}

// areaByName finds the area whose English name is name, ignoring case.
func areaByName(name string) (area, bool) {
	for a := area(0); a < areaCount; a++ {
		if strings.EqualFold(a.key(), name) {
			return a, true
		}
	}
	return areaOverview, false
}

// areaSections are the sections of each area in sub-tab order; the first one is what the area opens on.
var areaSections = [areaCount][]section{
	areaOverview:    {sectionOverview},
	areaLibrary:     {sectionServices, sectionCredentials},
	areaConnections: {sectionConnections},
	areaActivity:    {sectionLogs},
	areaAccess:      {sectionTokens, sectionApprovals},
	areaSettings:    {sectionGeneral, sectionVault, sectionDefaults, sectionTransfer, sectionUpdate, sectionSelftest},
}

// area is the area that holds s.
func (s section) area() area {
	for a := area(0); a < areaCount; a++ {
		for _, member := range areaSections[a] {
			if member == s {
				return a
			}
		}
	}
	return areaOverview
}

// subTitle is the name of s as a sub-tab.
func (s section) subTitle() string { return s.title() }

// index is the position of s among the sub-tabs of its area.
func (s section) index() int {
	for i, member := range areaSections[s.area()] {
		if member == s {
			return i
		}
	}
	return 0
}

// hasSubTabs says whether the active area shows a tab row.
func (m *Model) hasSubTabs() bool { return len(areaSections[m.section.area()]) > 1 }

// previewArea makes a the active area, on its first section, while the tab line keeps the focus.
func (m *Model) previewArea(a area) tea.Cmd {
	cmd := m.previewSection(areaSections[a][0])
	m.navZone = kittui.ZoneTabs
	return cmd
}

// advancedStart is the first Settings page after the device-local preferences: from it on the option bar
// shows a bar before the page, the part of Settings that manages the vault, the configuration and the
// program itself.
const advancedStart = sectionVault

// ref names s for a message that points at it: its sub-tab inside its area, with the digit that opens the
// area.
func (s section) ref() string {
	a := s.area()
	if len(areaSections[a]) == 1 {
		return fmt.Sprintf("%s (%d)", s.title(), int(a)+1)
	}
	return fmt.Sprintf("%s > %s (%d)", a.title(), s.title(), int(a)+1)
}
