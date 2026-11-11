package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func TestIconTableIsUniqueAndSafeToMeasure(t *testing.T) {
	seen := map[string]string{}
	meanings := map[string]bool{}
	for id := iconLogo; id < iconCount; id++ {
		e := iconTable[id]
		if e.meaning == "" || meanings[e.meaning] {
			t.Errorf("icon %d: meaning %q is empty or used twice", id, e.meaning)
		}
		meanings[e.meaning] = true
		for kind, glyph := range map[string]string{"unicode": e.unicode, "nerd": e.nerd} {
			if glyph == "" {
				t.Errorf("%s %s: no glyph", e.meaning, kind)
				continue
			}
			if other, dup := seen[kind+glyph]; dup {
				t.Errorf("%s %s: glyph %q is also used for %s", e.meaning, kind, glyph, other)
			}
			seen[kind+glyph] = e.meaning
			if err := kittui.CheckIcon(glyph); err != nil {
				t.Errorf("%s %s: %v", e.meaning, kind, err)
			}
		}
	}
}

func setUTF8(t *testing.T, term, locale string) {
	t.Helper()
	t.Setenv("TERM", term)
	t.Setenv("LC_ALL", locale)
}

func TestIconsFollowThePreference(t *testing.T) {
	setUTF8(t, "xterm-256color", "C.UTF-8")
	m, _ := lockedVaultModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	for value, want := range map[string]func(iconID) string{
		uiprefs.IconsUnicode:  func(id iconID) string { return iconTable[id].unicode },
		uiprefs.IconsNerdFont: func(id iconID) string { return iconTable[id].nerd },
		uiprefs.IconsNone:     func(iconID) string { return "" },
		"":                    func(iconID) string { return "" },
	} {
		m.prefs.Icons = value
		for id := iconLogo; id < iconCount; id++ {
			if got := m.icon(id); got != want(id) {
				t.Errorf("icons %q, %s: got %q, want %q", value, iconTable[id].meaning, got, want(id))
			}
		}
	}
	// Without a choice the model of a fresh device shows unicode icons.
	m.prefs = uiprefs.Default()
	if line := m.navLine(); !strings.Contains(line, iconTable[iconOverview].unicode) {
		t.Errorf("default tab line shows no unicode icon: %q", line)
	}
}

func TestNoIconsWithoutUTF8(t *testing.T) {
	for name, env := range map[string][2]string{"dumb": {"dumb", "C.UTF-8"}, "C locale": {"xterm-256color", "C"}} {
		t.Run(name, func(t *testing.T) {
			setUTF8(t, env[0], env[1])
			m, _ := lockedVaultModel(t)
			for _, value := range []string{uiprefs.IconsUnicode, uiprefs.IconsNerdFont} {
				m.prefs.Icons = value
				for id := iconLogo; id < iconCount; id++ {
					if got := m.icon(id); got != "" {
						t.Errorf("icons %q: %s shows %q", value, iconTable[id].meaning, got)
					}
				}
			}
		})
	}
}

func TestTwoCellIconsKeepEveryViewInsideTheTerminal(t *testing.T) {
	setUTF8(t, "xterm-256color", "C.UTF-8")
	m, _ := lockedVaultModel(t)
	m.prefs.Icons = uiprefs.IconsUnicode
	for _, size := range [][2]int{{40, 14}, {60, 20}, {80, 24}, {100, 28}, {140, 36}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.screen = screenNav
		assertViewFits(t, m.render(), size[0], size[1])
		press(t, m, ":")
		assertViewFits(t, m.render(), size[0], size[1])
		press(t, m, "esc")
	}
}
