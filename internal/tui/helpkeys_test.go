package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// namesHelp reports whether text names the help key.
func namesHelp(text string) bool {
	return strings.Contains(text, keyLabel(actHelp)+" "+tr(tHeaderHelp))
}

// Where the footer has to give up groups of keys, the help key stays.
func TestFooterKeepsTheHelpKeyWhereGroupsAreDropped(t *testing.T) {
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	m.screen = screenNav
	if foot := footerText(m); !namesHelp(ansi.Strip(foot)) {
		t.Errorf("full width footer = %q, want the help group", foot)
	}
	m.Update(tea.WindowSizeMsg{Width: minimumWidth, Height: 30})
	if full := flatText(strings.ReplaceAll(m.screenKeys, " · ", " ")); full == "" {
		t.Fatal("the screen names no keys")
	}
	if foot := ansi.Strip(footerText(m)); !namesHelp(foot) {
		t.Errorf("narrow footer = %q, want the help group", foot)
	}
}

// Opened from a screen, the first topic of the help ends with every group of that screen, also those the
// footer had to drop.
func TestHelpListsEveryKeyGroupOfTheScreenItWasOpenedFrom(t *testing.T) {
	check := func(t *testing.T, m *Model) {
		t.Helper()
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 80})
		m.render()
		if !helpOpen(m) {
			m.openHelp()
		}
		if m.screenKeys == "" {
			t.Fatal("the screen handed no keys to the footer")
		}
		m.render()
		press(t, m, "end")
		plain := ansi.Strip(m.render())
		at := strings.Index(plain, "This screen")
		if at < 0 {
			t.Fatalf("the help does not list the screen's keys:\n%s", plain)
		}
		groups := keymap.Hints(m.keyHints(m.screenKeys), kittui.InputNavigation, translator(false))
		if len(groups) == 0 {
			t.Fatal("the screen names no key group")
		}
		for _, g := range groups {
			if !strings.Contains(plain[at:], g.Text()) {
				t.Errorf("group %q is not listed:\n%s", g.Text(), plain[at:])
			}
		}
		assertViewFits(t, m.render(), 120, 80)
	}
	t.Run("list", func(t *testing.T) {
		m, _, _ := newModel(t)
		seedConnections(t, m, "a", "b")
		openSectionByName(t, m, sectionConnections)
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
		m.render()
		press(t, m, "?")
		if !helpOpen(m) {
			t.Fatalf("? opened screen %v", m.screen)
		}
		check(t, m)
	})
	t.Run("form", func(t *testing.T) {
		m, _, _ := newModel(t)
		seedConnections(t, m, "a")
		openSectionByName(t, m, sectionConnections)
		pressNew(t, m)
		m.screen = screenForm
		check(t, m)
	})
	t.Run("picker", func(t *testing.T) {
		reg := wikiRegistry(t)
		m, _ := toolsModel(t, reg, nil)
		openSectionByName(t, m, sectionConnections)
		pressNew(t, m)
		typeText(t, m, "fresh")
		focusField(t, m, accessLabel)
		selectChoice(t, m, accessTools)
		focusField(t, m, toolListLabel)
		press(t, m, "enter")
		if m.screen != screenPicker {
			t.Fatalf("screen %v, want the picker", m.screen)
		}
		check(t, m)
	})
}

// Opened through the command line, the help lists the keys of the screen the command line covered.
func TestHelpFromTheCommandLineListsTheCoveredScreen(t *testing.T) {
	m, _, _ := newModel(t)
	seedConnections(t, m, "a", "b")
	openSectionByName(t, m, sectionConnections)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m.render()
	want := m.screenKeys
	press(t, m, ":")
	m.render()
	if m.screen != screenCommand || m.screenKeys == want {
		t.Fatalf("the command line did not take over the footer: screen %v", m.screen)
	}
	typeText(t, m, "help")
	press(t, m, "enter")
	if !helpOpen(m) || m.screenKeys != want {
		t.Errorf("help from the command line lists %q on screen %v, want %q", m.screenKeys, m.screen, want)
	}
}
