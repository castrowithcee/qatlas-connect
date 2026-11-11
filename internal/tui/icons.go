package tui

import (
	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// iconID names the meaning of an icon. Every meaning has an entry of its own in iconTable, so no two things
// share a glyph.
type iconID int

const (
	// iconNone is the zero value: a thing without an icon.
	iconNone iconID = iota
	iconLogo
	iconOverview
	iconConnections
	iconActivity
	iconAccess
	iconLibrary
	iconSettings
	iconServices
	iconCredentials
	iconTokens
	iconApprovals
	iconUpdate
	iconCount
)

// iconEntry is one meaning with its glyph for each icons value. Every glyph passes kittui.CheckIcon: the
// unicode glyphs are emoji that draw two cells, the nerdfont glyphs are in the Font Awesome range and draw one.
type iconEntry struct {
	meaning string
	unicode string
	nerd    string
}

var iconTable = [iconCount]iconEntry{
	iconNone:        {},
	iconLogo:        {"logo", "\U0001F310", ""},
	iconOverview:    {"overview", "\U0001F3E0", ""},
	iconConnections: {"connections", "\U0001F517", ""},
	iconActivity:    {"activity", "\U0001F4C8", ""},
	iconAccess:      {"access", "\U0001F511", ""},
	iconLibrary:     {"library", "\U0001F4DA", ""},
	iconSettings:    {"settings", "\U0001F527", ""},
	iconServices:    {"services", "\U0001F4E6", ""},
	iconCredentials: {"credentials", "\U0001F510", ""},
	iconTokens:      {"tokens", "\U0001F3AB", ""},
	iconApprovals:   {"approvals", "✅", ""},
	iconUpdate:      {"update", "\U0001F389", ""},
}

// areaIcons is the meaning of each area's icon.
var areaIcons = [areaCount]iconID{
	areaOverview:    iconOverview,
	areaLibrary:     iconLibrary,
	areaConnections: iconConnections,
	areaActivity:    iconActivity,
	areaAccess:      iconAccess,
	areaSettings:    iconSettings,
}

// utf8Terminal reports whether the terminal is known to draw more than ASCII.
func utf8Terminal() bool { return terminalCharset() == kittui.CharsetUnicode }

// icon is the glyph of id under the icons preference, or "" where there is none: the preference is none or
// empty, or the terminal does not draw UTF-8. Callers put it beside the name, never in its place.
func (m *Model) icon(id iconID) string {
	if id == iconNone {
		return ""
	}
	switch m.prefs.Icons {
	case uiprefs.IconsUnicode:
		if utf8Terminal() {
			return iconTable[id].unicode
		}
	case uiprefs.IconsNerdFont:
		if utf8Terminal() {
			return iconTable[id].nerd
		}
	}
	return ""
}

// nerdAreaGlyphs are the area glyphs of the nerdfont value, shown as test characters.
func nerdAreaGlyphs() []string {
	glyphs := make([]string, 0, areaCount)
	for _, id := range areaIcons {
		glyphs = append(glyphs, iconTable[id].nerd)
	}
	return glyphs
}
