package tui

import (
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// PrefsSchema is the schema of the device-local preferences for the themes, the areas and the key table of
// the editor, as loading and saving them take it.
func PrefsSchema() kitprefs.Schema { return uiprefs.Schema(themeSet.Names(), startNames(), keymap) }

// defaultPrefs are the preferences of a device that has none.
func defaultPrefs() kitprefs.Prefs { return kitprefs.Default(PrefsSchema()) }

// startNames are the values of the start view: the English names of the areas in lower case.
func startNames() []string {
	var names []string
	for a := area(0); a < areaCount; a++ {
		names = append(names, strings.ToLower(a.key()))
	}
	return names
}

// prefsWarningLine is the first warning of loading the preferences in the language of the editor, or "".
func prefsWarningLine(warnings []kitprefs.Warning) string {
	if len(warnings) == 0 {
		return ""
	}
	return warningLine(warnings[0].Format(translator(false)))
}
