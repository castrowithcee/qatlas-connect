// Package uiprefs holds what is Connect's own in the device-local preferences file tui.yaml: the names of its
// themes and start view, and the earlier forms of the file that still load. The kit's uiprefs package reads,
// checks and writes the file.
package uiprefs

import (
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// The names of Connect's own themes.
const (
	ThemeQatlasDark  = "qatlas dark"
	ThemeQatlasLight = "qatlas light"
)

// The values a form offers for the mouse row.
const (
	ChoiceMouseOn  = "on"
	ChoiceMouseOff = "off"
)

// DefaultStart is the area the editor opens on by default.
const DefaultStart = "overview"

// formatVersion is written to every file a save writes. A file without it was written before unicode became
// the default icons value, when a save wrote the old default none explicitly; that none is not a choice.
// Earlier versions read the marker too, so they keep a none that was chosen.
const formatVersion = 2

// formerThemes are the values of the former colour scheme and the themes they stand for now.
var formerThemes = map[string]string{"auto": kittui.ThemeTerminal, "light": ThemeQatlasLight, "dark": ThemeQatlasDark}

// Schema is the schema of tui.yaml for the themes, the areas and the key table of the editor. It offers every
// navigation form of the kit, the tab line by default.
func Schema(themes, areas []string, keys *kittui.Keymap) kitprefs.Schema {
	return kitprefs.Schema{
		Themes: themes, Areas: areas, DefaultArea: DefaultStart, Keys: keys,
		Navigations: []string{kitprefs.NavigationTabs, kitprefs.NavigationPathLine, kitprefs.NavigationPathHeader,
			kitprefs.NavigationHeaderChoice},
		DefaultNavigation: kitprefs.NavigationTabs,
		Rewrite:           rewrite, WriteKeys: map[string]any{"format": formatVersion},
	}
}

// rewrite turns the earlier forms of the file into the current one before the kit checks a value.
func rewrite(raw map[string]any) {
	if theme, ok := raw["theme"].(string); ok {
		if current, former := formerThemes[theme]; former {
			raw["theme"] = current
		}
	}
	if format, _ := raw["format"].(int); raw["icons"] == kitprefs.IconsNone && format < formatVersion {
		raw["icons"] = kitprefs.IconsUnicode
	}
}
