package tui

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// restoreRenderer puts the process-wide colour state back after a test that changes it.
func restoreRenderer(t *testing.T) {
	t.Helper()
	colors, profile, saved := termColors, currentProfile(), styles
	t.Cleanup(func() {
		termColors = colors
		setProfile(profile)
		setTheme(saved)
	})
}

func TestColorSchemeNoColorWins(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileMono)
	termColors = kittui.TerminalColors{Foreground: "#ffffff", Background: "#000000"}
	for _, mode := range themeSet.Names() {
		applyTheme(mode)
		for name, st := range map[string]lipgloss.Style{
			"title": styles.title, "active": styles.active, "ok": styles.ok, "fail": styles.fail,
			"warning": styles.warning, "focusBorder": styles.focusBorder,
			"activeRow": styles.activeRow, "key": styles.key, "hint": styles.hint,
		} {
			out := st.Render("x")
			if strings.Contains(out, "38;") || strings.Contains(out, "48;") {
				t.Errorf("mode %s, style %s: NO_COLOR still emits a colour sequence: %q", mode, name, out)
			}
		}
	}
	// The row marker survives without colour: reverse video is an attribute, not a colour.
	if got := styles.activeRow.Render("x"); !strings.Contains(got, "7") {
		t.Errorf("activeRow lost reverse video: %q", got)
	}
}

func TestStartAreaOpensChosenArea(t *testing.T) {
	m, _, _ := newModel(t)
	if got, ok := areaByName("Connections"); !ok || got != areaConnections {
		t.Fatalf("areaByName(Connections) = %v, %v", got, ok)
	}
	if _, ok := areaByName("nowhere"); ok {
		t.Fatal("areaByName(nowhere) found an area")
	}
	m.start = areaConnections
	m.status = "warning: kept"
	m.Init()
	if m.section != sectionConnections {
		t.Errorf("section = %v, want connections", m.section)
	}
	if m.status != "warning: kept" {
		t.Errorf("status = %q; the opening notice was lost", m.status)
	}
}

func TestStartDefaultStaysOnOverview(t *testing.T) {
	m, _, _ := newModel(t)
	m.section = sectionOverview // what New itself starts on; the helper starts on Services
	m.Init()
	if m.section != sectionOverview {
		t.Errorf("section = %v, want overview", m.section)
	}
}

// NO_COLOR turns colour off with any value, also with one the colour library does not read as true: at a
// terminal the editor then keeps bold, faint and reverse and draws no colour sequence.
func TestNoColorWithAnyValueDrawsWithoutColour(t *testing.T) {
	restoreRenderer(t)
	// A terminal the colour library would otherwise give every colour.
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	if got := kittui.DetectColorProfile(io.Discard, os.Environ()); got != kittui.ProfileTrueColor {
		t.Fatalf("without NO_COLOR the profile is %v, want true colour", got)
	}
	for _, value := range []string{"1", "yes"} {
		t.Setenv("NO_COLOR", value)
		setProfile(kittui.DetectColorProfile(io.Discard, os.Environ()))
		applyTheme(uiprefs.ThemeQatlasDark)
		if toColorprofile(currentProfile()) != colorprofile.ASCII {
			t.Errorf("NO_COLOR=%s: profile %v, want ASCII", value, currentProfile())
		}
		m := sizedModel(groupModel(t), 100, 30)
		openSectionByName(t, m, sectionConnections)
		view := m.render()
		// Underline (4 alone) is an attribute: the search field of the filter bar is drawn underlined.
		if hasColour(view) {
			t.Errorf("NO_COLOR=%s: the screen carries a colour sequence:\n%q", value, view)
		}
		if !strings.Contains(view, "\x1b[1m") {
			t.Errorf("NO_COLOR=%s: the screen lost its attributes:\n%q", value, view)
		}
	}
}

// A saved start view names the area, not its position, so every area opens itself after a reordering.
func TestStartAreaSurvivesReordering(t *testing.T) {
	for a := area(0); a < areaCount; a++ {
		got, ok := areaByName(a.key())
		if !ok || got != a {
			t.Fatalf("areaByName(%q) = %v, %v, want %v", a.key(), got, ok, a)
		}
		m, _, _ := newModel(t)
		m.start = a
		m.Init()
		if a != areaOverview && m.section != areaSections[a][0] {
			t.Errorf("start %s opened section %v, want %v", a.key(), m.section, areaSections[a][0])
		}
	}
	want := []string{"Overview", "Library", "Connections", "Activity", "Access", "Settings"}
	for a, k := range want {
		if area(a).key() != k {
			t.Errorf("area %d = %s, want %s", a+1, area(a).key(), k)
		}
	}
}

// writePrefs writes data as tui.yaml in a new directory and returns its path.
func writePrefs(t *testing.T, data string) string {
	t.Helper()
	path := kitprefs.Path(t.TempDir())
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The files of earlier versions load under the schema of the editor without a warning.
func TestPrefsOfEarlierVersionsLoadWithoutWarning(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		want       kitprefs.Prefs
	}{
		{"v2.0.0", "theme: dark\nstart: library\nicons: none\nlanguage: english\nmouse: true\n",
			kitprefs.Prefs{Theme: uiprefs.ThemeQatlasDark, Start: "library", Icons: kitprefs.IconsUnicode,
				Language: kitprefs.LanguageEnglish, MouseEnabled: true}},
		{"v2.2.0 default", "theme: terminal\nstart: overview\nicons: unicode\nlanguage: english\nmouse: true\nformat: 2\n",
			defaultPrefs()},
		{"v2.2.0 changed", "theme: qatlas dark\nstart: overview\nicons: nerdfont\nlanguage: deutsch\nmouse: false\nformat: 2\n" +
			"keys:\n  info: f3\n  save: f4\n",
			kitprefs.Prefs{Theme: uiprefs.ThemeQatlasDark, Start: uiprefs.DefaultStart, Icons: kitprefs.IconsNerdFont,
				Language: kitprefs.LanguageGerman, Keys: map[action]string{actInfo: "f3", actSave: "f4"}}},
	} {
		got, warnings := kitprefs.LoadWarnings(writePrefs(t, tc.file), PrefsSchema())
		if !reflect.DeepEqual(got, tc.want) || len(warnings) != 0 {
			t.Errorf("%s: %+v %v, want %+v and no warning", tc.name, got, warnings, tc.want)
		}
	}
}

// A warning of loading the preferences is shown in the language of the editor. A start view in another
// spelling than the one the editor writes is a warning too.
func TestPrefsWarningsAreInTheLanguageOfTheEditor(t *testing.T) {
	for _, tc := range []struct{ file, english, german string }{
		{"theme: purple\n", `theme "purple" is not one of`, `theme "purple" ist keiner von`},
		{"start: nowhere\n", `start "nowhere" is not one of`, `start "nowhere" ist keiner von`},
		{"start: Library\n", `start "Library" is not one of`, `start "Library" ist keiner von`},
		{"keys:\n  save: x\n", "keys.save x: already the key of decline, using F2", "keys.save x: already the key of decline, verwendet wird F2"},
	} {
		path := writePrefs(t, tc.file)
		_, warnings := kitprefs.LoadWarnings(path, PrefsSchema())
		for lang, want := range map[i18n.Lang]string{i18n.English: "warning: " + path + ": " + tc.english, i18n.German: "Warnung: " + path + ": " + tc.german} {
			useLanguage(t, lang)
			if got := prefsWarningLine(warnings); !strings.HasPrefix(got, want) {
				t.Errorf("%q in %s: %q, want it to start with %q", tc.file, lang, got, want)
			}
		}
	}
	if got := prefsWarningLine(nil); got != "" {
		t.Errorf("without a warning: %q", got)
	}
}
