package tui

import (
	"image/color"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The theme set builds from the qatlas themes and the built-in schemes of the kit; newThemeSet panics otherwise.
func TestThemeSetBuilds(t *testing.T) {
	if _, err := kittui.NewThemeSet([]kittui.Theme{
		kittui.DarkTheme(uiprefs.ThemeQatlasDark), kittui.LightTheme(uiprefs.ThemeQatlasLight),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestColoursStepDownWithTheProfile(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileTrueColor)
	st := darkTheme().active
	for profile, want := range map[kittui.ColorProfile]string{
		kittui.ProfileTrueColor: "38;2;",
		kittui.ProfileANSI256:   "38;5;",
		kittui.ProfileANSI16:    "94",
	} {
		setProfile(profile)
		if out := shown(st.Render("x")); !strings.Contains(out, want) {
			t.Errorf("profile %v: %q lacks %q", profile, out, want)
		}
	}
}

func TestNoColorEmitsNoColourSequence(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileMono)
	applyTheme(uiprefs.ThemeQatlasDark)
	if styles.filled {
		t.Fatal("NO_COLOR left the background fill on")
	}
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.section = sectionOverview
	view := m.render()
	for _, seq := range []string{"\x1b[3", "\x1b[4", "38;", "48;"} {
		if strings.Contains(view, seq) {
			t.Errorf("NO_COLOR view holds %q", seq)
		}
	}
}

func TestQatlasBackgroundCoversEveryCell(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileTrueColor)
	t.Setenv("NO_COLOR", "")
	applyTheme(uiprefs.ThemeQatlasDark)
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.section = sectionOverview
	m.Init()
	lines := strings.Split(m.render(), "\n")
	if len(lines) != 24 {
		t.Fatalf("%d lines, want 24", len(lines))
	}
	p := styles.Palette()
	baseSeq, _, _ := strings.Cut(lipgloss.NewStyle().Foreground(lip(p.Text)).Background(lip(p.Base)).Render("x"), "x")
	if baseSeq == "" {
		t.Fatal("no canvas sequence under truecolor")
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 80 {
			t.Errorf("line %d is %d cells wide, want 80", i, w)
		}
		if !strings.HasPrefix(line, baseSeq) {
			t.Errorf("line %d does not open with the base colours", i)
		}
		// Every reset must be followed at once by the base colours.
		for _, reset := range []string{"\x1b[0m", "\x1b[m"} {
			for rest := line; ; {
				at := strings.Index(rest, reset)
				if at < 0 {
					break
				}
				rest = rest[at+len(reset):]
				if rest != "" && !strings.HasPrefix(rest, baseSeq) {
					t.Errorf("line %d: a reset is not followed by the base colours", i)
				}
			}
		}
	}
}

// darkTheme is the qatlas dark theme under the current profile, or true colour under a profile without colour,
// without painting the screen: the look most tests read their colours from.
func darkTheme() theme {
	p := currentProfile()
	if p.Colorless() {
		p = kittui.ProfileTrueColor
	}
	s, _ := themeSet.Resolve(uiprefs.ThemeQatlasDark, p, terminalCharset(), nil)
	th := themeOf(s)
	th.filled = false
	return th
}

// useColour gives the test a profile with colour and the dark theme, restored afterwards: the look of a
// terminal that shows colour. Tests start without colour (see profile).
func useColour(t *testing.T) {
	t.Helper()
	restoreRenderer(t)
	setProfile(kittui.ProfileTrueColor)
	setTheme(darkTheme())
}

// lookOf switches the profile and the dark theme with it, as a start does: the theme of a profile without
// colour names none.
func lookOf(p kittui.ColorProfile) {
	setProfile(p)
	useDarkColours()
}

// useDarkColours activates darkTheme, or no colour at all under a profile without colour, as applyTheme does.
func useDarkColours() {
	if noColour() {
		setTheme(noColorTheme())
		return
	}
	setTheme(darkTheme())
}

// hexColour is the colour of a "#rrggbb" string.
func hexColour(t *testing.T, hex string) color.RGBA {
	t.Helper()
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil || len(hex) != 7 {
		t.Fatalf("%q is not a hex colour", hex)
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// answerColours delivers the answers of a terminal with foreground fg and background bg to the model.
func answerColours(t *testing.T, m *Model, fg, bg string) {
	t.Helper()
	m.Update(tea.ForegroundColorMsg{Color: hexColour(t, fg)})
	m.Update(tea.BackgroundColorMsg{Color: hexColour(t, bg)})
}

// paletteOfTheme is the palette the kit resolves for a theme under a profile and the terminal's colours.
func paletteOfTheme(name string, profile kittui.ColorProfile, terminal *kittui.TerminalColors) kittui.Palette {
	s, _ := themeSet.Resolve(name, profile, kittui.CharsetUnicode, terminal)
	return s.Palette()
}

// Each qatlas theme paints the screen in its palette, the terminal theme never does.
func TestThemesPaintOnlyWhereTheyOwnTheBackground(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileTrueColor)
	t.Setenv("NO_COLOR", "")
	for name, want := range map[string]kittui.Theme{
		uiprefs.ThemeQatlasDark:  kittui.DarkTheme(uiprefs.ThemeQatlasDark),
		uiprefs.ThemeQatlasLight: kittui.LightTheme(uiprefs.ThemeQatlasLight),
	} {
		applyTheme(name)
		if p := want.Profiles[kittui.ProfileTrueColor]; !styles.filled || styles.Palette() != p {
			t.Errorf("%s: filled %v, palette base %q; want filled with base %q", name, styles.filled, styles.Palette().Base, p.Base)
		}
	}
	termColors = kittui.TerminalColors{Foreground: "#cdd6f4", Background: "#1e1e2e"}
	applyTheme(uiprefs.ThemeTerminal)
	if styles.filled {
		t.Error("the terminal theme paints the screen")
	}
	if got := themeOrDefault("no such theme"); got != uiprefs.ThemeTerminal {
		t.Errorf("an unknown name gives %q, want the terminal theme", got)
	}
}

// The terminal theme leaves the text to the terminal and names its accents by ANSI slot, so the terminal's own
// scheme colours them; nothing paints the ground of the screen.
func TestTerminalThemeTakesTheColoursOfTheTerminal(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileTrueColor)
	t.Setenv("NO_COLOR", "")
	m, _, _ := newModel(t)
	m.prefs = uiprefs.Default()
	applyTheme(m.prefs.Theme)
	if styles.Palette() != paletteOfTheme(uiprefs.ThemeTerminal, kittui.ProfileTrueColor, nil) || styles.Palette().Panel != "" {
		t.Fatalf("before the answer the palette is %+v, want the plain one", styles.Palette())
	}
	answerColours(t, m, "#cdd6f4", "#1e1e2e")
	p := styles.Palette()
	if styles.filled || p.Text != "" || p.Base != "#1e1e2e" || p.Panel == "" {
		t.Errorf("filled %v, text %q, base %q, panel %q; want no fill, the terminal's text, its background and a panel step",
			styles.filled, p.Text, p.Base, p.Panel)
	}
	for role, want := range map[string]string{"accent": "\x1b[1;34m", "ok": "\x1b[1;32m", "fail": "\x1b[1;31m", "warning": "\x1b[1;33m"} {
		st := map[string]lipgloss.Style{"accent": styles.active, "ok": styles.ok, "fail": styles.fail, "warning": styles.warning}[role]
		if got := st.Render("x"); !strings.HasPrefix(got, want) {
			t.Errorf("%s = %q, want the ANSI slot %q", role, got, want)
		}
	}
	if got := styles.title.Render("x"); strings.Contains(got, "38;") || strings.Contains(got, "\x1b[3") {
		t.Errorf("the title names a text colour: %q", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.section = sectionOverview
	if view := m.render(); strings.Contains(view, "48;2;30;30;46") {
		t.Error("the view paints the background of the terminal")
	}
}

// A choice of a qatlas theme keeps its palette when the terminal answers late.
func TestTerminalAnswerLeavesOtherThemes(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileTrueColor)
	t.Setenv("NO_COLOR", "")
	m, _, _ := newModel(t)
	m.prefs = uiprefs.Default()
	m.prefs.Theme = uiprefs.ThemeQatlasLight
	applyTheme(m.prefs.Theme)
	answerColours(t, m, "#cdd6f4", "#1e1e2e")
	if styles.Palette() != kittui.LightTheme(uiprefs.ThemeQatlasLight).Profiles[kittui.ProfileTrueColor] || !styles.filled {
		t.Errorf("the answer replaced the qatlas light theme: %+v", styles.Palette())
	}
}

// Without an answer, with 16 colours or with a pair that is itself too faint, the terminal theme has no fills:
// panels stand by their frames, and the selection is reverse video behind its marker.
func TestTerminalThemeFallsBackToFrames(t *testing.T) {
	restoreRenderer(t)
	t.Setenv("NO_COLOR", "")
	for _, tc := range []struct {
		name    string
		profile kittui.ColorProfile
		colors  kittui.TerminalColors
	}{
		{"no answer", kittui.ProfileTrueColor, kittui.TerminalColors{}},
		{"only a background", kittui.ProfileTrueColor, kittui.TerminalColors{Background: "#000000"}},
		{"16 colours", kittui.ProfileANSI16, kittui.TerminalColors{Foreground: "#ffffff", Background: "#000000"}},
		{"a pair below 4.5:1", kittui.ProfileTrueColor, kittui.TerminalColors{Foreground: "#657b83", Background: "#fdf6e3"}},
	} {
		setProfile(tc.profile)
		termColors = tc.colors
		applyTheme(uiprefs.ThemeTerminal)
		p := styles.Palette()
		if p.Panel != "" || p.Surface != "" || p.Selection != "" || p.Base != "" || styles.filled {
			t.Errorf("%s: palette %+v, want no fills", tc.name, p)
		}
		if got := styles.activeRow.Render("x"); !strings.Contains(got, "7") || strings.Contains(got, "48;") {
			t.Errorf("%s: the selection %q is not reverse video", tc.name, got)
		}
		if selectionMark() != ">" {
			t.Errorf("%s: the selection lost its marker", tc.name)
		}
	}
}

// A profile without colour (NO_COLOR at a terminal included) wins over the terminal theme: no colour, and no
// question to the terminal.
func TestNoColorAsksTheTerminalNothing(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileTrueColor)
	if terminalColourQuery(uiprefs.ThemeTerminal) == nil {
		t.Fatal("the terminal theme does not ask the terminal for its colours")
	}
	if terminalColourQuery(uiprefs.ThemeQatlasDark) != nil {
		t.Error("a qatlas theme asks the terminal for its colours")
	}
	setProfile(kittui.ProfileANSI16)
	if terminalColourQuery(uiprefs.ThemeTerminal) != nil {
		t.Error("a 16-colour terminal is asked for colours it cannot use")
	}
	for _, p := range []kittui.ColorProfile{kittui.ProfileMono, kittui.ProfileNone} {
		setProfile(p)
		if terminalColourQuery(uiprefs.ThemeTerminal) != nil {
			t.Errorf("profile %v: the terminal is asked for its colours", p)
		}
		termColors = kittui.TerminalColors{Foreground: "#ffffff", Background: "#000000"}
		applyTheme(uiprefs.ThemeTerminal)
		if styles.Palette() != (kittui.Palette{}) || strings.Contains(styles.active.Render("x"), "\x1b[3") {
			t.Errorf("profile %v: the terminal theme still names colours", p)
		}
	}
}

// The registry offers the terminal theme first, and every name it holds is a theme of its own.
func TestThemeRegistry(t *testing.T) {
	names := themeSet.Names()
	if len(names) < 3 || strings.Join(names[:3], ",") != "terminal,qatlas dark,qatlas light" {
		t.Errorf("themes = %v", names)
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Errorf("theme %q is registered twice", name)
		}
		seen[name] = true
		if got, warning := knownTheme(name); got != name || warning != "" {
			t.Errorf("knownTheme(%q) = %q, %q", name, got, warning)
		}
	}
	got, warning := knownTheme("purple")
	if got != uiprefs.ThemeTerminal || !strings.Contains(warning, `theme "purple"`) {
		t.Errorf("knownTheme(purple) = %q, %q; want terminal and a warning", got, warning)
	}
}
