package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/selfupdate"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// clickChoice clicks the middle of the row of entry i of the open list modal, as the frame drawn last placed it.
func clickChoice(t *testing.T, m *Model, i int) {
	t.Helper()
	m.render()
	for _, target := range m.modalTargets {
		if target.Item == string(choiceID(i)) {
			clickAt(t, m, target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y)
			return
		}
	}
	t.Fatalf("entry %d of the list is no target: %+v", i, m.modalTargets)
}

// shownIndexes are the indexes of the entries a list built by newChoice shows now, in order.
func shownIndexes(l *kittui.List) []int {
	var out []int
	for _, id := range l.Shown() {
		i, _ := strconv.Atoi(string(id))
		out = append(out, i)
	}
	return out
}

// choiceOpen reports whether the list of screen s stands as the modal over the screen it was opened from.
func choiceOpen(m *Model, s screen) bool {
	list, _, _ := m.screenChoice()
	if m.screen != s || list == nil {
		return false
	}
	bottom := m.modal
	if len(m.modalsUnder) > 0 {
		bottom = m.modalsUnder[0].part
	}
	shown, ok := bottom.(*choiceModal)
	return ok && shown.List == list
}

func shownProviders(m *Model) []string {
	var ids []string
	for _, i := range shownIndexes(m.providers.list) {
		ids = append(ids, m.providers.ids[i])
	}
	return ids
}

func markedProvider(m *Model) string {
	id, _ := m.providers.selected()
	return id
}

func shownTemplates(m *Model) []string {
	var names []string
	for _, i := range shownIndexes(m.template.list) {
		names = append(names, m.template.names[i])
	}
	return names
}

func shownVersions(m *Model) []string {
	var tags []string
	for _, i := range shownIndexes(m.versions.list) {
		tags = append(tags, m.versions.entries[i].Version)
	}
	return tags
}

func markedVersion(m *Model) string {
	if i, ok := chosen(m.versions.list, len(m.versions.entries)); ok {
		return m.versions.entries[i].Version
	}
	return ""
}

// clickButton clicks the middle of the button of act of the open list modal.
func clickButton(t *testing.T, m *Model, act action) {
	t.Helper()
	m.render()
	for _, target := range m.modalTargets {
		if target.Action == act && strings.HasPrefix(target.Item, "\x00B") {
			clickAt(t, m, target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y)
			return
		}
	}
	t.Fatalf("the list has no button %q: %+v", act, m.modalTargets)
}

// choiceState is what a list may not change before it is answered: the screen under it, the form, the
// configuration and what the updater installed.
func choiceState(m *Model, fake *versionFake) string {
	installed := 0
	if fake != nil {
		installed = len(fake.installed) + fake.updates
	}
	return fmt.Sprintf("section %v form %q editing %q wizard %v connections %d installed %d updating %v",
		m.section, m.formState(), m.editing, m.wizard != nil, len(m.cfg.Connections), installed, m.updating)
}

// Cancel, esc after a typed search and a click beside the frame close each list and leave everything as it
// was: the form and its rows, the configuration, and the running version.
func TestEveryWayOutOfAChoiceListLeavesTheState(t *testing.T) {
	for _, list := range choiceLists() {
		for _, way := range []string{"cancel", "esc", "beside"} {
			t.Run(list.name+"/"+way, func(t *testing.T) {
				m := list.open(t)
				var fake *versionFake
				if list.screen == screenVersions {
					fake = m.updater.(*versionFake)
				}
				m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
				if !choiceOpen(m, list.screen) {
					t.Fatalf("the list is not open: screen %v", m.screen)
				}
				// The screen the list answers to is the one it was opened over, or the question before the entries.
				back := m.underScreen()
				if list.name == "template entries" {
					back = screenTemplate
				}
				before := choiceState(m, fake)
				press(t, m, "down")
				switch way {
				case "cancel":
					clickButton(t, m, actBack)
				case "esc":
					if list.search {
						typeText(t, m, "x")
						press(t, m, "esc")
					}
					press(t, m, "esc")
				case "beside":
					clickAt(t, m, 0, m.termHeight/2)
				}
				if m.screen != back || list.name != "template entries" && m.modal != nil {
					t.Fatalf("%s left screen %v with a modal %v, want screen %v", way, m.screen, m.modal != nil, back)
				}
				if got := choiceState(m, fake); got != before {
					t.Errorf("%s changed the state:\n%s\nwant\n%s", way, got, before)
				}
			})
		}
	}
}

// evil is a name with a control sequence that would set the clipboard and clear the screen.
const evil = "evil\x1b]52;c;eA==\x07\x1b[2Jname"

// A provider ID, an entry name and a version tag reach the terminal through the log filter: the control
// characters are drawn as replacement characters.
func TestAChoiceListDrawsControlCharactersHarmless(t *testing.T) {
	cases := map[string]func(t *testing.T) *Model{
		"provider table": func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 120, 40)
			openSectionByName(t, m, sectionServices)
			pressNew(t, m)
			focusField(t, m, providerLabel)
			m.fields[m.focus].choices = append(m.fields[m.focus].choices, evil)
			press(t, m, "enter")
			return m
		},
		"template entries": func(t *testing.T) *Model {
			m, _ := templateModel(t)
			m.cfg.Connections[evil] = config.Connection{Service: evil, Credential: "reader"}
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			m.screen = screenNav
			press(t, m, "g", "down", "enter")
			return m
		},
		"version list": func(t *testing.T) *Model {
			fake := choosable()
			fake.entries = append(fake.entries, selfupdate.Entry{Version: evil, Older: true})
			m := startedVersions(t, fake)
			deliver(m, m.openVersions())
			return m
		},
	}
	for name, open := range cases {
		t.Run(name, func(t *testing.T) {
			m := open(t)
			if m.modal == nil {
				t.Fatalf("the list did not open: screen %v fail %q", m.screen, m.fail)
			}
			view := m.render()
			if strings.Contains(view, "\x1b]52") || strings.Contains(view, "\x07") || strings.Contains(view, "\x1b[2J") {
				t.Fatalf("the control characters of a name reach the terminal:\n%q", view)
			}
			if !strings.Contains(ansi.Strip(view), "evil�]52;c;eA==��[2Jname") {
				t.Errorf("the name is not drawn with its control characters replaced:\n%s", ansi.Strip(view))
			}
		})
	}
}

// The entries of the template list of Credentials show where the secrets are kept, never a value, and the
// form a chosen entry fills holds none of them.
func TestTheTemplateListShowsAndCopiesNoSecret(t *testing.T) {
	m, _, _, _, mem := newStoreModel(t)
	mustNoError(t, m.cfg.SetService("wiki", config.Service{Provider: "bookstack", BaseURL: "https://wiki.example.invalid"}))
	mustNoError(t, m.cfg.SetCredential("reader", config.Credential{Provider: "bookstack", Type: config.CredentialTypeKeyring}))
	mustNoError(t, m.store.Save(m.cfg))
	syncRevision(t, m)
	mustNoError(t, mem.Set(secret.StoreKey("reader", "token-id"), "canary-original-id"))
	mustNoError(t, mem.Set(secret.StoreKey("reader", "token-secret"), "canary-original-secret"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	openSectionByName(t, m, sectionCredentials)
	m.screen = screenList
	press(t, m, "n", "enter", "down", "enter")
	if !choiceOpen(m, screenTemplate) || !m.template.picking {
		t.Fatalf("the entries did not open: screen %v", m.screen)
	}
	if view := m.render(); !strings.Contains(view, "reader") || strings.Contains(view, "canary-") {
		t.Fatalf("the entries show a secret value or miss the credential:\n%s", ansi.Strip(view))
	}
	press(t, m, "enter")
	if m.screen != screenForm || m.fieldValue("name") != "reader-copy" || m.editing != "" {
		t.Fatalf("enter: screen %v name %q editing %q, want the form of a new copy", m.screen, m.fieldValue("name"), m.editing)
	}
	if view := screenOf(m); strings.Contains(view, "canary-") || strings.Contains(m.formState(), "canary-") {
		t.Errorf("the copied form holds a secret value:\n%s", view)
	}
}

// Each list fits the sizes the editor supports, over the screen it was opened from or in the workspace, with
// its marked row and its keys on screen.
func TestEveryChoiceListFitsEverySize(t *testing.T) {
	for _, list := range choiceLists() {
		for _, size := range [][2]int{{minimumWidth, minimumHeight}, {59, 19}, {80, 24}, {150, 42}} {
			t.Run(fmt.Sprintf("%s %dx%d", list.name, size[0], size[1]), func(t *testing.T) {
				m := list.open(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				view := m.render()
				assertViewFits(t, view, size[0], size[1])
				plain := ansi.Strip(view)
				if !choiceOpen(m, list.screen) || strings.Contains(plain, "Resize terminal") ||
					!strings.Contains(plain, "enter") || !strings.Contains(plain, "esc") {
					t.Errorf("the list at %dx%d:\n%s", size[0], size[1], plain)
				}
				if it, ok := shownList(m).Current(); !ok || !strings.Contains(plain, it.Cells[0]) {
					t.Errorf("the marked row is not on screen at %dx%d:\n%s", size[0], size[1], plain)
				}
			})
		}
	}
}

func shownList(m *Model) *kittui.List {
	list, _, _ := m.screenChoice()
	return list
}
