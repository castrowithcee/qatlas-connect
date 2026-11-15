package tui

import (
	"slices"

	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// themeSet holds the themes the General page offers: the terminal theme first, then the qatlas themes, then
// the built-in schemes of the kit.
var themeSet = newThemeSet()

func newThemeSet() *kittui.ThemeSet {
	set, err := kittui.NewThemeSet([]kittui.Theme{
		kittui.DarkTheme(uiprefs.ThemeQatlasDark),
		kittui.LightTheme(uiprefs.ThemeQatlasLight),
	})
	if err != nil {
		// The themes are fixed data, so this is a programming error a test catches.
		panic(err)
	}
	return set
}

// themeOrDefault is the theme the preference names, or the default where this version has no theme of that
// name.
func themeOrDefault(name string) string {
	names := themeSet.Names()
	if slices.Contains(names, name) {
		return name
	}
	return names[0]
}

// knownTheme is the theme the preference names, or the default with a warning where this version has no theme
// of that name.
func knownTheme(name string) (string, string) {
	if got := themeOrDefault(name); got != name {
		return got, tr(tThemeUnknown, name, got)
	}
	return name, ""
}

// theme is the active look: the styles the kit resolved for the theme, the colour profile and the character
// set, with their text styles under the names the screens use. None of them carries meaning on its own: focus
// has its marker and its border, a result its "[ok]", "[failed]", "warning:" or "error:" prefix, and a
// terminal without colour, or with NO_COLOR set, loses nothing.
type theme struct {
	kittui.Styles
	title       lipgloss.Style
	active      lipgloss.Style
	hint        lipgloss.Style
	ok          lipgloss.Style
	fail        lipgloss.Style
	warning     lipgloss.Style
	focusBorder lipgloss.Style
	quietBorder lipgloss.Style
	// activeRow highlights the marked row of a table screen (a section list, the picker, the provider
	// table) the full width of the row. It never carries the selection alone: the mark of listRow (an accent
	// bar under colour, '>' without) still leads the row, so NO_COLOR or an ASCII terminal keeps showing which one
	// is marked.
	activeRow lipgloss.Style
	// cell is the text of a table cell that carries content (names, urls, descriptions); the dimmed cells use
	// hint. selMark is the narrow accent bar that leads the marked row, on the colour of the bar.
	cell    lipgloss.Style
	selMark lipgloss.Style
	// key is the key of a footer hint (bold); the action after it keeps the plain hint style.
	key lipgloss.Style
	// footKey is the key of the footer line: the accent colour, bold.
	footKey lipgloss.Style
	// filled says the whole screen carries the base colour (see Paint); it starts as Paints of the styles.
	filled bool
}

// themeOf names the text styles of resolved styles as the screens use them.
func themeOf(s kittui.Styles) theme {
	return theme{
		Styles:      s,
		title:       s.Style(kittui.StyleTitle),
		active:      s.Style(kittui.StyleActive),
		hint:        s.Style(kittui.StyleHint),
		ok:          s.Style(kittui.StyleOK),
		fail:        s.Style(kittui.StyleFail),
		warning:     s.Style(kittui.StyleWarn),
		focusBorder: s.Style(kittui.StyleBorderFocus),
		quietBorder: s.Style(kittui.StyleBorder),
		activeRow:   s.Style(kittui.StyleActiveRow),
		cell:        s.Style(kittui.StyleText),
		selMark:     s.Style(kittui.StyleSelectionMark),
		key:         s.Style(kittui.StyleKey),
		footKey:     s.Style(kittui.StyleFooterKey),
		filled:      s.Paints(),
	}
}

// styles is the active theme. Until Run applies one it is the theme without colour.
var styles = noColorTheme()

// noColorTheme names no colour at all and keeps only the attributes (bold, faint, reverse), as NO_COLOR at a
// terminal does.
func noColorTheme() theme {
	s, _ := themeSet.Resolve(uiprefs.ThemeTerminal, kittui.ProfileMono, terminalCharset(), nil)
	return themeOf(s)
}

func setTheme(t theme) { styles = t }

// applyTheme activates the theme of a name; a name no theme has gives the default. A profile without colour
// (NO_COLOR included) wins over every theme, and the editor asks the terminal nothing then (see
// terminalColourQuery).
func applyTheme(name string) {
	s, _ := themeSet.Resolve(name, currentProfile(), terminalCharset(), &termColors)
	setTheme(themeOf(s))
}
