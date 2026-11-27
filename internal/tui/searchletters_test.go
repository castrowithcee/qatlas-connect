package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// searchScreens opens each screen whose search line is always there, with the query of its line.
func searchScreens(t *testing.T) map[string]func() (*Model, func() string) {
	return map[string]func() (*Model, func() string){
		"tool picker": func() (*Model, func() string) {
			m := connectionForm(t, helpPaneMinWidth-1)
			focusField(t, m, accessLabel)
			selectChoice(t, m, accessTools)
			focusField(t, m, toolListLabel)
			press(t, m, "enter")
			return m, func() string { return m.picker.query() }
		},
		"provider table": func() (*Model, func() string) {
			m, _ := toolsModel(t, wikiRegistry(t), nil)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
			openProviderRow(t, m, sectionConnections)
			press(t, m, "enter")
			return m, func() string { return m.providers.list.Query() }
		},
		"version list": func() (*Model, func() string) {
			m := startedVersions(t, choosable())
			openVersionList(t, m)
			return m, func() string { return m.versions.list.Query() }
		},
		"template picker": func() (*Model, func() string) {
			m, _ := templateModel(t, "alpha", "beta")
			openSectionByName(t, m, sectionConnections)
			m.screen = screenList
			press(t, m, "n", "down", "enter")
			if m.template == nil || !m.template.picking {
				t.Fatal("the template picker did not open")
			}
			return m, func() string { return m.template.list.Query() }
		},
		"target picker": func() (*Model, func() string) {
			m := pickerModel(t, &listSource{list: pickPage(group("a"))})
			openPicker(t, m)
			return m, func() string { return m.targetPick.top().list.query() }
		},
		"target add menu": func() (*Model, func() string) {
			m := pickerModel(t, &listSource{list: pickPage(group("a"))})
			return m, func() string { return m.targetAdd.choices.query() }
		},
	}
}

// On every screen with a fixed search line, i and c are text even as the first character.
func TestInfoAndCopyLettersAreTextOnEverySearchLine(t *testing.T) {
	for name, open := range searchScreens(t) {
		t.Run(name, func(t *testing.T) {
			m, query := open()
			if !m.typing() && m.modal == nil && name != "target add menu" {
				t.Fatalf("screen %v does not read text", m.screen)
			}
			scr := m.screen
			_, cmd := m.Update(textKey("i"))
			if infoOpen(m) || query() != "i" {
				t.Fatalf("i: overlay %v, query %q, want the letter typed", infoOpen(m), query())
			}
			if cmd != nil {
				if _, ok := clipboardOf(cmd); ok {
					t.Fatal("i handed text to the clipboard")
				}
			}
			press(t, m, "backspace")
			got, copied := pressCopy(m)
			if copied || infoOpen(m) || query() != "c" || m.screen != scr {
				t.Fatalf("c: copied %q (%v), overlay %v, query %q, want the letter typed", got, copied, infoOpen(m), query())
			}
		})
	}
}

// ? and : keep acting on an empty search line, except in a kit list that filters as typed, which reads them as
// text from the first character on.
func TestHelpAndCommandStillOpenOnAnEmptySearchLine(t *testing.T) {
	for name, open := range searchScreens(t) {
		t.Run(name, func(t *testing.T) {
			m, query := open()
			if name == "tool picker" {
				t.Skip("the tool picker never opened the help from its search line")
			}
			press(t, m, "?")
			if m.modal != nil && !helpOpen(m) {
				if query() != "?" {
					t.Fatalf("? in the kit list: query %q, want it typed", query())
				}
				return
			}
			if !helpOpen(m) {
				t.Fatalf("? left screen %v, want the help", m.screen)
			}
		})
		t.Run(name+" command", func(t *testing.T) {
			m, query := open()
			if name == "tool picker" {
				t.Skip("the tool picker never opened the command line from its search line")
			}
			press(t, m, ":")
			if m.modal != nil {
				if query() != ":" {
					t.Fatalf(": in the kit list: query %q, want it typed", query())
				}
				return
			}
			if m.screen != screenCommand {
				t.Fatalf(": left screen %v, want the command line", m.screen)
			}
		})
	}
}

// An own info key still opens the explanation on a search line.
func TestInfoExtraKeyOpensTheExplanationOnASearchLine(t *testing.T) {
	keepKeys(t)
	applyKeys(map[action]string{actInfo: "f3"})
	m := connectionForm(t, helpPaneMinWidth-1)
	focusField(t, m, accessLabel)
	selectChoice(t, m, accessTools)
	focusField(t, m, toolListLabel)
	press(t, m, "enter")
	if m.screen != screenPicker {
		t.Fatalf("screen %v, want the tool picker", m.screen)
	}
	press(t, m, "f3")
	if !infoOpen(m) {
		t.Fatalf("the extra info key opened nothing:\n%s", m.render())
	}
}
