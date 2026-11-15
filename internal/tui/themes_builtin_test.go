package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// builtinThemeNames are the built-in schemes of the kit, offered after the terminal and the qatlas themes.
func builtinThemeNames(t *testing.T) []string {
	t.Helper()
	names := themeSet.Names()
	if len(names) < 13 {
		t.Fatalf("%d themes, want the terminal, the two qatlas themes and the built-in schemes", len(names))
	}
	return names[3:]
}

// With true colour a scheme paints the ground of its own background; with 16 colours it is the terminal's
// fallback, paints nothing and shows the selection in reverse video.
func TestBuiltinThemesFollowTheColourProfile(t *testing.T) {
	restoreRenderer(t)
	t.Setenv("NO_COLOR", "")
	for _, name := range builtinThemeNames(t) {
		setProfile(kittui.ProfileANSI16)
		applyTheme(name)
		if styles.filled || styles.Palette().Base != "" || !styles.activeRow.GetReverse() {
			t.Errorf("%s with 16 colours: want the terminal fallback without a painted ground", name)
		}
		setProfile(kittui.ProfileTrueColor)
		applyTheme(name)
		if !styles.filled || !strings.HasPrefix(string(styles.Palette().Base), "#") {
			t.Errorf("%s with truecolor: filled %v base %q", name, styles.filled, styles.Palette().Base)
		}
	}
}

func TestBuiltinThemesYieldToNoColor(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileMono)
	for _, name := range builtinThemeNames(t) {
		applyTheme(name)
		if styles.filled || styles.Palette() != (kittui.Palette{}) {
			t.Errorf("%s under NO_COLOR keeps a palette or paints", name)
		}
	}
}

func TestUnknownSchemeNameFallsBackToTerminal(t *testing.T) {
	for _, name := range []string{"dracula", "solarized dark", "Nord"} {
		got, warning := knownTheme(name)
		if got != uiprefs.ThemeTerminal || !strings.Contains(warning, name) {
			t.Errorf("knownTheme(%q) = %q, %q; want terminal and a warning", name, got, warning)
		}
	}
}

// Every built-in theme is offered on the General page and stays chosen after the save and a reload.
func TestBuiltinThemesSurviveSaveAndReload(t *testing.T) {
	for _, name := range builtinThemeNames(t) {
		t.Run(name, func(t *testing.T) {
			m, path := lockedVaultModel(t)
			t.Setenv("NO_COLOR", "")
			setProfile(kittui.ProfileTrueColor)
			openSectionByName(t, m, sectionGeneral)
			target := -1
			for i, c := range m.fields[0].choices {
				if c == name {
					target = i
				}
			}
			if target < 0 {
				t.Fatalf("the theme row does not offer %q", name)
			}
			for i := 0; i < target; i++ {
				press(t, m, "right")
			}
			pump(t, m, "f2")
			if m.fail != "" || !strings.HasPrefix(m.status, "Saved") {
				t.Fatalf("status %q fail %q, want it saved", m.status, m.fail)
			}
			got, warnings := uiprefs.Load(path)
			if got.Theme != name || len(warnings) != 0 {
				t.Fatalf("stored theme = %q %v", got.Theme, warnings)
			}
			if want := paletteOfTheme(name, kittui.ProfileTrueColor, nil); !styles.filled || styles.Palette() != want {
				t.Errorf("the saved theme is not applied")
			}
			m2, _ := lockedVaultModel(t)
			m2.prefs = got
			openSectionByName(t, m2, sectionGeneral)
			if f := m2.fields[0]; f.choices[f.index] != name {
				t.Errorf("after reload the row shows %q", f.choices[f.index])
			}
		})
	}
}
