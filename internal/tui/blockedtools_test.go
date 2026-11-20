package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// moveTo moves the picker cursor down to the first row the test accepts.
func moveTo(t *testing.T, m *Model, want func(string) bool) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if choice, ok := m.picker.selected(); ok && want(choice) {
			return
		}
		press(t, m, "down")
	}
	t.Fatal("the picker has no such row")
}

func isToolRow(choice string) bool { return !strings.HasPrefix(choice, "\x00") }

func savedBlocked(t *testing.T, path, name string) []string {
	t.Helper()
	cfg, err := config.Load(path, wikiRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Connections[name].BlockedTools
}

// The tool picker tells the three states apart in text: ticked is offered, a blocked tool that is not ticked
// carries a marker of its own, and ticking it says that its block ends on save.
func TestToolPickerShowsOfferedNotOfferedAndBlocked(t *testing.T) {
	m, _ := toolsModel(t, wikiRegistry(t), map[string]config.Connection{"wiki": {Service: "wiki", Credential: "reader",
		Tools: []string{"bookstack.pages.get"}, BlockedTools: []string{"bookstack.pages.list"}}})
	openEntryForm(t, m, sectionConnections, "wiki")
	focusField(t, m, toolListLabel)
	press(t, m, "enter")

	typeText(t, m, "pages.list")
	moveTo(t, m, isToolRow)
	view := strings.Join(strings.Fields(screenOf(m)), " ")
	if !strings.Contains(view, "[ ] List BookStack pages bookstack.pages.list read ⊘ blocked") {
		t.Fatalf("blocked tool is not marked:\n%s", view)
	}
	press(t, m, " ")
	view = strings.Join(strings.Fields(screenOf(m)), " ")
	if !strings.Contains(view, "[x] List BookStack pages bookstack.pages.list read (block ends on save)") || strings.Contains(view, "⊘") {
		t.Fatalf("ticking a blocked tool does not show that its block ends:\n%s", view)
	}
	press(t, m, "backspace", "backspace", "backspace", "backspace")
	typeText(t, m, "get")
	view = strings.Join(strings.Fields(screenOf(m)), " ")
	if !strings.Contains(view, "[x] Get a BookStack page bookstack.pages.get") || strings.Contains(view, "⊘") || strings.Contains(view, "block ends") {
		t.Fatalf("an offered tool carries a block marker:\n%s", view)
	}
}

// Ticking a blocked tool and saving removes its block.
func TestTickingABlockedToolEndsItsBlockOnSave(t *testing.T) {
	m, path := toolsModel(t, wikiRegistry(t), map[string]config.Connection{"wiki": {Service: "wiki", Credential: "reader",
		Tools: []string{"bookstack.pages.get"}, BlockedTools: []string{"bookstack.pages.list", "bookstack.pages.create"}}})
	openEntryForm(t, m, sectionConnections, "wiki")
	focusField(t, m, toolListLabel)
	press(t, m, "enter")
	typeText(t, m, "pages.list")
	moveTo(t, m, isToolRow)
	press(t, m, " ", "enter")
	pump(t, m, "f2")
	if m.fail != "" {
		t.Fatalf("save failed: %s", m.fail)
	}
	if got := savedBlocked(t, path, "wiki"); !reflect.DeepEqual(got, []string{"bookstack.pages.create"}) {
		t.Fatalf("saved blocked_tools = %v", got)
	}
}

// Blocking and unblocking work in both access modes, and the row lists what is blocked.
func TestBlockingToolsInBothModes(t *testing.T) {
	for _, mode := range []string{accessEffects, accessTools} {
		t.Run(mode, func(t *testing.T) {
			conn := config.Connection{Service: "wiki", Credential: "reader"}
			if mode == accessTools {
				conn.Tools = []string{"bookstack.pages.get"}
			}
			m, path := toolsModel(t, wikiRegistry(t), map[string]config.Connection{"wiki": conn})
			openEntryForm(t, m, sectionConnections, "wiki")
			if m.field(blockedLabel) == nil || m.field(blockedLabel).hidden {
				t.Fatal("the blocked row is not shown")
			}
			focusField(t, m, blockedLabel)
			press(t, m, "enter")
			typeText(t, m, "pages.create")
			moveTo(t, m, isToolRow)
			press(t, m, " ", "enter")
			if got := m.fieldValue(blockedLabel); got != "bookstack.pages.create" {
				t.Fatalf("blocked row = %q", got)
			}
			if view := screenOf(m); !strings.Contains(view, "1 blocked: bookstack.pages.create") {
				t.Fatalf("the row does not list the blocked tool:\n%s", view)
			}
			pump(t, m, "f2")
			if m.fail != "" {
				t.Fatalf("save failed: %s", m.fail)
			}
			if got := savedBlocked(t, path, "wiki"); !reflect.DeepEqual(got, []string{"bookstack.pages.create"}) {
				t.Fatalf("saved blocked_tools = %v", got)
			}

			openEntryForm(t, m, sectionConnections, "wiki")
			focusField(t, m, blockedLabel)
			press(t, m, "enter")
			typeText(t, m, "pages.create")
			moveTo(t, m, isToolRow)
			if view := strings.Join(strings.Fields(screenOf(m)), " "); !strings.Contains(view, "[x]") {
				t.Fatalf("a blocked tool is not ticked in the picker:\n%s", view)
			}
			press(t, m, " ", "enter")
			pump(t, m, "f2")
			if got := savedBlocked(t, path, "wiki"); len(got) != 0 {
				t.Fatalf("blocked_tools after unblocking = %v", got)
			}
		})
	}
}

// The picker lists only tools the form does not offer, and a whole group is stored as one prefix.
func TestBlockedPickerListsNotOfferedToolsAndStoresGroupsAsPrefix(t *testing.T) {
	m, path := toolsModel(t, wikiRegistry(t), map[string]config.Connection{"wiki": {Service: "wiki", Credential: "reader",
		Tools: []string{"bookstack.pages.get"}}})
	openEntryForm(t, m, sectionConnections, "wiki")
	focusField(t, m, blockedLabel)
	if got := m.blockCandidates(); slices.Contains(got, "bookstack.pages.get") || !slices.Contains(got, "bookstack.pages.list") {
		t.Fatalf("candidates = %v", got)
	}
	// A change of the tool list changes what the blocked picker offers.
	focusField(t, m, toolListLabel)
	press(t, m, "enter")
	typeText(t, m, "pages.list")
	moveTo(t, m, isToolRow)
	press(t, m, " ", "enter")
	if slices.Contains(m.blockCandidates(), "bookstack.pages.list") {
		t.Fatal("a tool the form now offers can still be blocked")
	}
	// Untick everything, so a whole group of tools is not offered.
	press(t, m, "enter")
	moveTo(t, m, func(c string) bool { return c == allGroupsRow })
	press(t, m, " ")
	if m.pickerMarks["bookstack.pages.get"] {
		press(t, m, " ")
	}
	press(t, m, "enter")
	focusField(t, m, blockedLabel)
	press(t, m, "enter")
	typeText(t, m, "bookstack.pages.")
	moveTo(t, m, func(c string) bool { return strings.HasPrefix(c, groupRowPrefix) })
	press(t, m, " ", "enter")
	if got := m.field(blockedLabel).marked(); !reflect.DeepEqual(got, []string{"bookstack.pages.*"}) {
		t.Fatalf("blocked entries = %v, want the group as one prefix", got)
	}
	if view := screenOf(m); !strings.Contains(view, "prefix, tools today") {
		t.Fatalf("the row does not say what the prefix covers:\n%s", view)
	}
	pump(t, m, "f2")
	if m.fail != "" {
		t.Fatalf("save failed: %s", m.fail)
	}
	if got := savedBlocked(t, path, "wiki"); !reflect.DeepEqual(got, []string{"bookstack.pages.*"}) {
		t.Fatalf("saved blocked_tools = %v", got)
	}
}

// The help of the row says that blocked locks no tool.
func TestBlockedRowHelpExplainsItLocksNothing(t *testing.T) {
	m, _ := toolsModel(t, wikiRegistry(t), map[string]config.Connection{"wiki": {Service: "wiki", Credential: "reader"}})
	openEntryForm(t, m, sectionConnections, "wiki")
	focusField(t, m, blockedLabel)
	if detail := m.fieldDetail(*m.field(blockedLabel)); !strings.Contains(detail, "locks no tool") {
		t.Fatalf("detail = %q", detail)
	}
}
