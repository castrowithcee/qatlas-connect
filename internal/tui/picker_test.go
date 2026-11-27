package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// seedConnections stores one service, one credential and the named connections over them, so a test can
// start from a choice row longer than any terminal.
func seedConnections(t *testing.T, m *Model, names ...string) {
	t.Helper()
	mustNoError(t, m.cfg.SetService("wiki", config.Service{
		Provider: "bookstack", BaseURL: "https://wiki.example.invalid",
	}))
	mustNoError(t, m.cfg.SetCredential("reader", config.Credential{
		Provider: "bookstack", Type: config.CredentialTypeKeyring,
	}))
	for _, name := range names {
		mustNoError(t, m.cfg.SetConnection(name, config.Connection{Service: "wiki", Credential: "reader"}))
	}
	mustNoError(t, m.store.Save(m.cfg))
	syncRevision(t, m)
	m.configExists = true
}

func numberedConnections(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("conn-%03d", i)
	}
	return names
}

// openDefaultForm opens the form of a new default and focuses its connection row.
func openDefaultForm(t *testing.T, m *Model) {
	t.Helper()
	openSectionByName(t, m, sectionDefaults)
	press(t, m, "n")
	typeText(t, m, "docs")
	focusField(t, m, "connection")
}

// valuePickerOpen reports whether the kit's list of values stands over the form.
func valuePickerOpen(m *Model) bool {
	return m.valuePick != nil && m.modal != nil && m.screen == screenForm
}

// pickerPosition is where the selection of the open value picker stands, by the rule of the kit's list.
func pickerPosition(t *testing.T, m *Model) string {
	t.Helper()
	if !valuePickerOpen(m) {
		t.Fatalf("no value picker is open: screen %v", m.screen)
	}
	return m.valuePick.list.Position()
}

// countingPress presses keys and returns how many it pressed, so a test can bound the effort of a choice.
func countingPress(t *testing.T, m *Model, keys ...string) int {
	t.Helper()
	press(t, m, keys...)
	return len(keys)
}

// Any one of many values is reached by typing a part of it, not by stepping through all values before it,
// and the choice is saved like one made with left/right. The key line names enter on the row whatever its size.
func TestThePickerReachesOneOfManyConnectionsDirectly(t *testing.T) {
	m, _, path := newModel(t)
	names := numberedConnections(60)
	seedConnections(t, m, names...)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	openDefaultForm(t, m)
	if got := m.fieldValue("connection"); got != names[0] {
		t.Fatalf("connection = %q, want the first value %q", got, names[0])
	}
	if view := screenOf(m); !strings.Contains(view, "enter choose") {
		t.Errorf("a row of %d values does not point out the picker:\n%s", len(names), view)
	}

	keys := countingPress(t, m, "/")
	if !valuePickerOpen(m) {
		t.Fatalf("/ on a choice row opened screen %v, want the picker", m.screen)
	}
	view := screenOf(m)
	for _, want := range []string{"Choose connection", "> conn-000  current"} {
		if !strings.Contains(view, want) {
			t.Errorf("the picker does not show %q:\n%s", want, view)
		}
	}
	if got := pickerPosition(t, m); got != "1/60" {
		t.Errorf("position %q, want 1/60", got)
	}
	typeText(t, m, "047")
	keys += 3
	view = screenOf(m)
	if got := pickerPosition(t, m); got != "1/1 (60 total)" || !strings.Contains(view, "> conn-047") {
		t.Errorf("the filtered picker at %q does not select conn-047:\n%s", got, view)
	}
	keys += countingPress(t, m, "enter")
	if keys >= 10 {
		t.Errorf("choosing took %d keys", keys)
	}
	if m.screen != screenForm || m.valuePick != nil || m.modal != nil {
		t.Fatalf("enter left screen %v with the picker open, want the form", m.screen)
	}
	if got := m.fieldValue("connection"); got != "conn-047" {
		t.Fatalf("connection = %q, want conn-047", got)
	}

	pump(t, m, "f2")
	if m.fail != "" {
		t.Fatalf("saving reported %q", m.fail)
	}
	saved, err := loadTestConfig(t, path)
	mustNoError(t, err)
	if got := saved.Defaults.Connections["docs"]; got != "conn-047" {
		t.Errorf("stored default = %q, want conn-047", got)
	}
}

// The filter ignores case, keeps the order of the row and gives the same answer every time.
func TestThePickerFilterIgnoresCaseAndIsDeterministic(t *testing.T) {
	m, _, _ := newModel(t)
	seedConnections(t, m, append(numberedConnections(50), "Wiki-Main", "wiki-archive", "chat")...)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	openDefaultForm(t, m)
	// The row lists its values sorted, so the capitalised name comes first.
	for _, query := range []string{"WIKI", "wiki", "WiKi"} {
		press(t, m, "/")
		typeText(t, m, query)
		if got := pickerPosition(t, m); got != "1/2 (53 total)" {
			t.Errorf("filter %q stands at %q, want 1/2 (53 total)", query, got)
		}
		view := screenOf(m)
		main, archive := strings.Index(view, "> Wiki-Main"), strings.Index(view, "wiki-archive")
		if main < 0 || archive < main || strings.Contains(view, "chat") || strings.Contains(view, "conn-000") {
			t.Errorf("filter %q draws the wrong values:\n%s", query, view)
		}
		press(t, m, "esc", "esc")
	}
}

// Escape clears a typed filter first and then leaves the row and every row that depends on it exactly as they
// were. Enter takes the one value that is selected and shown, and a filter that matches nothing takes nothing.
func TestThePickerCancelsCompletelyAndTakesOnlyAShownValue(t *testing.T) {
	m, _, _ := newModel(t)
	seedConnections(t, m, numberedConnections(60)...)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	openDefaultForm(t, m)
	press(t, m, "right", "right")
	before := m.fieldValue("connection")
	if before != "conn-002" {
		t.Fatalf("connection = %q, want conn-002", before)
	}

	press(t, m, "/", "down", "down", "down")
	typeText(t, m, "conn-01")
	press(t, m, "down", "esc")
	if !valuePickerOpen(m) || m.valuePick.list.Query() != "" {
		t.Fatalf("the first esc left screen %v, want the picker with its filter cleared", m.screen)
	}
	press(t, m, "esc")
	if m.screen != screenForm || m.modal != nil {
		t.Fatalf("esc left screen %v, want the form", m.screen)
	}
	if got := m.fieldValue("connection"); got != before {
		t.Errorf("connection after esc = %q, want %q unchanged", got, before)
	}
	if got := m.fieldValue("domain"); got != "docs" {
		t.Errorf("domain after esc = %q, want the typed docs", got)
	}

	// No match: enter keeps the picker open and changes nothing.
	press(t, m, "/")
	typeText(t, m, "nothing-like-this")
	if view := screenOf(m); !strings.Contains(view, "No entry matches") || pickerPosition(t, m) != "0/0 (60 total)" {
		t.Errorf("the empty picker does not say so:\n%s", view)
	}
	press(t, m, "enter")
	if !valuePickerOpen(m) {
		t.Errorf("enter without a match left the picker for screen %v", m.screen)
	}
	press(t, m, "esc", "esc")
	if got := m.fieldValue("connection"); got != before {
		t.Errorf("connection after an empty enter = %q, want %q unchanged", got, before)
	}

	// Enter takes the selected value among those shown, and only that one.
	press(t, m, "/")
	typeText(t, m, "conn-04")
	// A page is what the frame drawn last showed.
	screenOf(m)
	press(t, m, "pgup", "down", "down")
	view := screenOf(m)
	if !strings.Contains(view, "> conn-042") || strings.Contains(view, "conn-050") {
		t.Fatalf("the picker does not show the selection among the matches:\n%s", view)
	}
	press(t, m, "enter")
	if got := m.fieldValue("connection"); got != "conn-042" {
		t.Errorf("connection = %q, want the selected conn-042", got)
	}
}

// A small row keeps its direct way: left/right changes it in place, no dialog opens, and the key line names
// it next to enter. A provider row is the exception: it is always chosen in its table.
func TestASmallChoiceKeepsLeftAndRight(t *testing.T) {
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	openSectionByName(t, m, sectionCredentials)
	pressNew(t, m)
	focusField(t, m, storageLabel)
	before := m.credentialType()
	press(t, m, "right")
	if m.screen != screenForm {
		t.Fatalf("right opened screen %v, want the form", m.screen)
	}
	after := m.credentialType()
	if after == before {
		t.Fatalf("right left the type at %q", after)
	}
	press(t, m, "left")
	if got := m.credentialType(); got != before {
		t.Errorf("left moved to %q, want back to %q", got, before)
	}
	if view := screenOf(m); !strings.Contains(flatText(view), "enter choose") || !strings.Contains(view, "←/→ switch") {
		t.Errorf("the key line does not name both ways:\n%s", view)
	}
}

// The picker scrolls inside a small terminal like every list: the selection stays on screen and the resize
// notice never replaces it. Up and down wrap round, the page keys stop at the ends. A terminal too small even
// for that leads back to the form, not out of it.
func TestThePickerFitsASmallTerminal(t *testing.T) {
	m, _, _ := newModel(t)
	names := numberedConnections(60)
	seedConnections(t, m, names...)
	// The form is opened in a usual terminal: the empty list before it names the long path of the test
	// directory, which needs more than twelve lines.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	openDefaultForm(t, m)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: minimumHeight})
	if view := screenOf(m); strings.Contains(view, "Resize terminal") {
		t.Fatalf("the form does not fit 40x13:\n%s", view)
	}
	press(t, m, "/")

	check := func(want string) {
		t.Helper()
		view := screenOf(m)
		if strings.Contains(view, "Resize terminal") {
			t.Fatalf("the picker was replaced by the resize notice:\n%s", view)
		}
		assertViewFits(t, view, 40, minimumHeight)
		if !strings.Contains(view, "> "+want) {
			t.Fatalf("the selection %q is not on screen:\n%s", want, view)
		}
	}
	check(names[0])
	for i := 1; i < 15; i++ {
		press(t, m, "down")
		check(names[i])
	}
	press(t, m, "end")
	check(names[59])
	press(t, m, "home")
	check(names[0])
	press(t, m, "pgup", "pgup", "pgup")
	check(names[0])
	press(t, m, "up")
	check(names[59])
	press(t, m, "pgdown")
	check(names[59])
	press(t, m, "down")
	check(names[0])

	m.Update(tea.WindowSizeMsg{Width: 20, Height: 6})
	press(t, m, "esc")
	if m.screen != screenForm {
		t.Errorf("esc on the resize notice left screen %v, want the form", m.screen)
	}
	if got := m.fieldValue("connection"); got != names[0] {
		t.Errorf("connection = %q, want %q unchanged", got, names[0])
	}
}

// Enter and / open the values of every kind of choice row in every editor, the way enter opens the
// provider table, and none of them saves the form; esc returns to the row unchanged.
func TestEveryChoiceRowOpensOnEnterAndSlash(t *testing.T) {
	reg := wikiRegistry(t)
	rows := []struct {
		section section
		label   string
	}{
		{sectionServices, providerLabel},
		{sectionCredentials, providerLabel},
		{sectionCredentials, storageLabel},
		{sectionConnections, providerLabel},
		{sectionConnections, "service"},
		{sectionConnections, "credential"},
		{sectionConnections, profileLabel},
		{sectionConnections, effectsLabel},
		{sectionConnections, accessLabel},
		{sectionConnections, toolListLabel},
		{sectionDefaults, "connection"},
	}
	for _, row := range rows {
		for _, key := range []string{"enter", "/"} {
			m, path := toolsModel(t, reg, map[string]config.Connection{"wiki": {Service: "wiki", Credential: "reader"}})
			openSectionByName(t, m, row.section)
			pressNew(t, m)
			typeText(t, m, "fresh")
			switch row.label {
			case effectsLabel:
				selectAccess(t, m, accessEffects)
			case toolListLabel:
				selectAccess(t, m, accessTools)
			}
			focusField(t, m, row.label)
			before, err := os.ReadFile(path)
			mustNoError(t, err)
			value := m.fieldValue(row.label)

			pump(t, m, key)
			switch {
			case row.label == providerLabel && m.screen != screenProviders,
				row.label == toolListLabel && m.screen != screenPicker,
				row.label != providerLabel && row.label != toolListLabel && !valuePickerOpen(m):
				t.Fatalf("%q on %v %s opened screen %v", key, row.section, row.label, m.screen)
			}
			if after, err := os.ReadFile(path); err != nil || string(after) != string(before) || m.fail != "" {
				t.Fatalf("%q on %v %s wrote the file or failed: %v %q", key, row.section, row.label, err, m.fail)
			}
			press(t, m, "esc")
			if m.screen != screenForm || m.fields[m.focus].label != row.label || m.fieldValue(row.label) != value {
				t.Fatalf("esc after %q on %v %s left screen %v on %q with %q", key, row.section, row.label,
					m.screen, m.fields[m.focus].label, m.fieldValue(row.label))
			}
		}
	}
}

// F2 saves from a choice row, where enter opens the values instead.
func TestF2SavesFromAChoiceRow(t *testing.T) {
	reg := wikiRegistry(t)
	m, path := toolsModel(t, reg, nil)
	openSectionByName(t, m, sectionConnections)
	pressNew(t, m)
	typeText(t, m, "fresh")
	selectAccess(t, m, accessEffects)
	focusField(t, m, effectsLabel)
	if view := screenOf(m); !strings.Contains(flatText(view), "enter open F2 save") && strings.Contains(view, "tab next field") {
		t.Errorf("the key line of a multiselect row does not name its keys:\n%s", view)
	}
	pump(t, m, "f2")
	if m.fail != "" {
		t.Fatalf("F2 reported %q", m.fail)
	}
	if got := savedConnection(t, path, reg, "fresh"); got.Service != "wiki" {
		t.Fatalf("F2 saved %+v, want the new connection", got)
	}
}

// The permissions are ticked in the same picker as the tools: space ticks, enter keeps, esc drops, and
// default stays exclusive of the explicit permissions.
func TestThePermissionsAreTickedInThePicker(t *testing.T) {
	reg := wikiRegistry(t)
	m, _ := toolsModel(t, reg, nil)
	openSectionByName(t, m, sectionConnections)
	pressNew(t, m)
	selectAccess(t, m, accessEffects)
	focusField(t, m, effectsLabel)
	// The recommended profile selects tools, so the effects start on the provider default; tick read here.
	m.field(effectsLabel).selected = map[string]bool{"read": true}
	if got := m.fieldValue(effectsLabel); got != "read" {
		t.Fatalf("effects = %q, want read ticked", got)
	}
	press(t, m, "enter")
	if view := flatText(screenOf(m)); !strings.Contains(view, "[x] read") || !strings.Contains(view, "enter keep") ||
		!strings.Contains(view, "space toggle") {
		t.Fatalf("the permissions picker does not show its ticks and keys:\n%s", view)
	}
	typeText(t, m, "create")
	press(t, m, " ", "esc", "esc")
	if m.screen != screenLeave || !strings.Contains(screenOf(m), "the ticks changed") {
		t.Fatalf("esc dropped changed ticks without asking: screen %v", m.screen)
	}
	press(t, m, "d")
	if got := m.fieldValue(effectsLabel); m.screen != screenForm || got != "read" {
		t.Fatalf("discarding kept a tick: %q", got)
	}
	press(t, m, "enter", "esc")
	if m.screen != screenForm || m.modal != nil {
		t.Fatalf("esc on unchanged ticks asked: screen %v", m.screen)
	}

	press(t, m, "enter")
	typeText(t, m, "create")
	press(t, m, " ", "enter")
	if got := m.fieldValue(effectsLabel); got != "read, create" {
		t.Fatalf("permissions = %q, want read, create", got)
	}
	press(t, m, "/")
	typeText(t, m, "default")
	press(t, m, " ", "enter")
	if got := m.fieldValue(effectsLabel); got != "" {
		t.Fatalf("default kept explicit permissions: %q", got)
	}
	press(t, m, "enter")
	typeText(t, m, "read")
	press(t, m, " ", "enter")
	if got := m.fieldValue(effectsLabel); got != "read" {
		t.Fatalf("an explicit permission kept default: %q", got)
	}
}

// In the guided setup enter and / open a choice row and stay on the step; F2 goes on from it.
func TestAChoiceRowDoesNotAdvanceTheSetup(t *testing.T) {
	m, _, _, _, _ := newStoreModel(t)
	walkSetup(t, m, stepService)
	if got := m.fields[m.focus].label; got != "service" {
		t.Fatalf("the service step opened on %q", got)
	}
	for _, key := range []string{"enter", "/"} {
		press(t, m, key)
		if !valuePickerOpen(m) || m.wizard.step != stepService {
			t.Fatalf("%q on the service row opened screen %v at step %d", key, m.screen, m.wizard.step)
		}
		press(t, m, "esc")
		if m.screen != screenForm || m.modal != nil || m.wizard.step != stepService {
			t.Fatalf("esc in the picker left screen %v at step %d", m.screen, m.wizard.step)
		}
	}
	if view := screenOf(m); !strings.Contains(flatText(view), "enter choose F2 next") && strings.Contains(view, "←/→ switch") && strings.Contains(view, "tab next field") {
		t.Errorf("the key line does not name F2 next:\n%s", view)
	}
	press(t, m, "tab")
	typeText(t, m, "wiki")
	press(t, m, "tab")
	typeText(t, m, "https://wiki.example.invalid")
	press(t, m, "shift+tab", "shift+tab", "f2")
	if m.wizard.step != stepCredential || m.fail != "" {
		t.Fatalf("F2 on the service row went to step %d, error %q", m.wizard.step, m.fail)
	}
}

// A picker has no letter actions, so it filters as you type and shows "Type to search" until something is
// typed. Its marked row is highlighted the full width of the row under colour
// and, without it, is still told apart by its leading '>' alone; an unmarked row beside it still gets its
// unshaded under colour. No row is ever wider than the terminal, at any of the widths the design is
// meant to hold at.
func TestPickerRowsAreHighlightedFullWidthReadWithoutColourAndSearchByTyping(t *testing.T) {
	before := currentProfile()
	t.Cleanup(func() { setProfile(before) })

	for _, width := range []int{40, 79, 80, 120} {
		m, _, _ := newModel(t)
		names := numberedConnections(30)
		seedConnections(t, m, names...)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		openDefaultForm(t, m)
		press(t, m, "/")
		if !valuePickerOpen(m) {
			t.Fatalf("width %d: / did not open the picker: screen %v", width, m.screen)
		}
		marked, alt := names[0], names[1]

		setProfile(kittui.ProfileNone)
		if view := screenOf(m); !strings.Contains(view, "Type to search") {
			t.Errorf("width %d: the empty picker shows no 'Type to search' placeholder:\n%s", width, view)
		}

		setProfile(kittui.ProfileANSI16)
		coloured := screenOf(m)
		assertViewFits(t, coloured, width, m.termHeight)
		if row := findRow(coloured, marked); !strings.Contains(row, "\x1b[") {
			t.Errorf("width %d: the marked picker row shows no colour: %q\n%s", width, row, coloured)
		}
		if row := findRow(coloured, alt); strings.Contains(row, "\x1b[") {
			t.Errorf("width %d: the unmarked picker row carries colour, so it is not shaded: %q\n%s", width, row, coloured)
		}

		setProfile(kittui.ProfileNone)
		plain := screenOf(m)
		assertViewFits(t, plain, width, m.termHeight)
		if strings.Contains(plain, "\x1b") {
			t.Errorf("width %d: NO_COLOR still emits escape sequences in the picker:\n%s", width, plain)
		}
		if row := findRow(plain, marked); !strings.HasPrefix(row, "> ") {
			t.Errorf("width %d: the marked picker row has no leading '>' without colour: %q\n%s", width, row, plain)
		}

		typeText(t, m, "015")
		if got := pickerPosition(t, m); got != "1/1 (30 total)" {
			t.Errorf("width %d: typing does not filter the picker to conn-015: %q", width, got)
		}
		if view := screenOf(m); !strings.Contains(view, "> conn-015") {
			t.Errorf("width %d: the filtered picker does not mark conn-015:\n%s", width, view)
		}
		press(t, m, "esc")
	}
}
