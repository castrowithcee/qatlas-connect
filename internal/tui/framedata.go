package tui

import (
	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// The parts of the kit's frame that stand before anything is drawn: the areas and their tabs, the active
// ones, and the status marks of the header.

// kitAreas are the areas of the tab line with the sections of each as its tabs. The digit of an area is its
// place on the line.
func (m *Model) kitAreas() []kit.Area {
	areas := make([]kit.Area, 0, areaCount)
	for a := area(0); a < areaCount; a++ {
		ka := kit.Area{ID: kit.AreaID(a.key()), Title: areaTitles[a], Icon: m.icon(areaIcons[a])}
		for i, s := range areaSections[a] {
			ka.Tabs = append(ka.Tabs, kit.Tab{ID: kit.TabID(s.key()), Title: sectionTitles[s], GroupStart: i > 0 && s == advancedStart})
		}
		areas = append(areas, ka)
	}
	return areas
}

// kitActive is the active area and tab, and the text of the mode the guided setup stands in outside every area.
func (m *Model) kitActive() (kit.AreaID, kit.TabID, textID) {
	var mode textID
	if m.wizard != nil {
		mode = tTabSetup
	}
	return kit.AreaID(m.section.area().key()), kit.TabID(m.section.key()), mode
}

// kitMarks are the status marks of the header as the kit takes them. A mark with no status of its own, the
// admin state of an unlocked vault, carries its words alone.
func (m *Model) kitMarks() []kit.StatusMark {
	var marks []kit.StatusMark
	for _, mark := range m.vaultMarks() {
		km := kit.StatusMark{Text: mark.text}
		if mark.short != "" {
			km.Short = []string{mark.short}
		}
		switch mark.kind {
		case statusOK:
			km.Kind = kit.StatusOK
		case statusWarn:
			km.Kind = kit.StatusWarn
		case statusFail:
			km.Kind = kit.StatusFail
		}
		if mark.urgent {
			km.Rank = kit.MarkBeforeKeys
		}
		marks = append(marks, km)
	}
	return marks
}
