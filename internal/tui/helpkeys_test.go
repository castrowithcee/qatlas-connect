package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// lastLine is the footer of a rendered frame.
func lastLine(m *Model) string {
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	return strings.TrimRight(lines[len(lines)-1], " ")
}

// namesHelp reports whether text names the help key, plain or as the hint to the keys that did not fit.
func namesHelp(text string) bool {
	return strings.Contains(text, keyLabel(actHelp)+" "+tr(tHeaderHelp)) || strings.Contains(text, keyLabel(actHelp)+" "+tr(tHeaderHelpAll))
}

// The help group of the footer says "all keys" only where groups of keys had to give way.
func TestFooterHintsAtTheHelpWhereGroupsAreDropped(t *testing.T) {
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	m.screen = screenNav
	hint := keyLabel(actHelp) + " " + tr(tHeaderHelpAll)
	if last := lastLine(m); strings.Contains(last, tr(tHeaderHelpAll)) || !strings.Contains(last, keyLabel(actHelp)+" "+tr(tHeaderHelp)) {
		t.Errorf("full width footer = %q, want the plain help group", last)
	}
	m.Update(tea.WindowSizeMsg{Width: minimumWidth, Height: 30})
	m.render()
	if full := flatText(strings.ReplaceAll(m.screenKeys, " · ", " ")); full == "" {
		t.Fatal("the screen names no keys")
	}
	if last := lastLine(m); !strings.Contains(last, hint) {
		t.Errorf("narrow footer = %q, want %q", last, hint)
	}
}

// Opened from a screen, the first section of the help lists every group of that screen, also those the
// footer had to drop.
func TestHelpListsEveryKeyGroupOfTheScreenItWasOpenedFrom(t *testing.T) {
	check := func(t *testing.T, m *Model) {
		t.Helper()
		if m.screen != screenHelp {
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
			m.render()
			m.openHelp()
		}
		keys := m.helpKeysText
		if keys == "" {
			t.Fatal("the screen handed no keys to the footer")
		}
		lines := m.helpLines()
		if len(lines) == 0 || lines[0] != tr(tScreenKeysHeading) || !isHeading(lines[0]) {
			t.Fatalf("the help does not start with the screen's keys:\n%s", strings.Join(lines, "\n"))
		}
		text := strings.Join(lines, "\n")
		for _, group := range strings.Split(m.footerKeys(keys), " · ") {
			key, action, _ := strings.Cut(group, " ")
			want := "- " + action + ": " + key
			if action == "" {
				want = "- " + key
			}
			if !strings.Contains(text, want) {
				t.Errorf("group %q is not listed as %q:\n%s", group, want, text)
			}
		}
		assertViewFits(t, m.render(), 60, 24)
	}
	t.Run("list", func(t *testing.T) {
		m, _, _ := newModel(t)
		seedConnections(t, m, "a", "b")
		openSectionByName(t, m, sectionConnections)
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
		m.render()
		press(t, m, "?")
		if m.screen != screenHelp {
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
	if m.screen != screenHelp || m.helpKeysText != want {
		t.Errorf("help from the command line lists %q on screen %v, want %q", m.helpKeysText, m.screen, want)
	}
}
