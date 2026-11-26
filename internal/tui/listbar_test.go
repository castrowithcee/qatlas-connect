package tui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The values of the filter bar change what the list shows and in which order, and leave the configuration
// exactly as it is.
func TestFilterBarShapesTheListAndNeverTheConfiguration(t *testing.T) {
	m := newTableModel(t, 100)
	conn := m.cfg.Connections["tracker-triage"]
	conn.Disabled = true
	m.cfg.Connections["tracker-triage"] = conn
	before := fmt.Sprintf("%#v", *m.cfg)
	openSectionByName(t, m, sectionConnections)
	if got := shownEntries(m); !reflect.DeepEqual(got, []string{"wiki-docs", "tracker-issues", "tracker-triage"}) {
		t.Fatalf("default list = %v", got)
	}

	press(t, m, "tab")
	if focusedBarField(m) != "Group" || !strings.Contains(barLineOf(m, 80), ">  Group: Provider ▾") {
		t.Fatalf("tab did not reach the Group field: bar %q, line %q", focusedBarField(m), barLineOf(m, 80))
	}
	press(t, m, "right")
	if listGrouped(m) || len(listRowsOf(m)) != 3 {
		t.Fatalf("Group none: groups %v, rows %v", listGrouped(m), listRowsOf(m))
	}
	press(t, m, "left")
	if !listGrouped(m) {
		t.Fatal("Group provider did not bring the groups back")
	}

	press(t, m, "tab", "right")
	if got := shownEntries(m); !reflect.DeepEqual(got, []string{"wiki-docs", "tracker-issues"}) {
		t.Fatalf("State active = %v", got)
	}
	press(t, m, "right")
	if got := shownEntries(m); !reflect.DeepEqual(got, []string{"tracker-triage"}) {
		t.Fatalf("State inactive = %v", got)
	}
	press(t, m, "right")
	if got := shownEntries(m); len(got) != 3 {
		t.Fatalf("State all = %v", got)
	}

	// Sorted by provider and ungrouped, the providers stand in order, the names inside each of them.
	press(t, m, "tab", "right")
	press(t, m, "shift+tab", "shift+tab", "right")
	if got := shownEntries(m); !reflect.DeepEqual(got, []string{"wiki-docs", "tracker-issues", "tracker-triage"}) {
		t.Fatalf("sorted by provider, ungrouped = %v", got)
	}
	press(t, m, "tab", "tab", "left")
	press(t, m, "shift+tab", "shift+tab", "left")
	if got := shownEntries(m); !reflect.DeepEqual(got, []string{"wiki-docs", "tracker-issues", "tracker-triage"}) {
		t.Fatalf("grouped again = %v", got)
	}

	if after := fmt.Sprintf("%#v", *m.cfg); after != before {
		t.Error("the filter bar changed the configuration")
	}
	if m.dirty() {
		t.Error("the filter bar left the editor with unsaved changes")
	}
}

// Credentials and services have no state, so their bar has two fields; left/right on the rows still fold.
func TestFilterBarOfStatelessListsHasNoStateField(t *testing.T) {
	m := newTableModel(t, 100)
	openSectionByName(t, m, sectionCredentials)
	if got := barKinds(m.section); len(got) != 2 {
		t.Fatalf("Credentials bar fields = %v", got)
	}
	if line := barLineOf(m, 100); strings.Contains(line, "State") {
		t.Errorf("Credentials bar shows a state: %q", line)
	}
	selectRow(m, groupRow("tracker"))
	press(t, m, "left")
	if !groupFolded(m, "tracker") {
		t.Error("left on the rows no longer folds the group")
	}
	press(t, m, "tab", "tab", "tab")
	if m.screen != screenNav {
		t.Errorf("tab after the last field = screen %v, want the tab line", m.screen)
	}
}

// The detail pane appears from detailPaneMinWidth and not below it.
func TestDetailPaneAppearsAtItsMinimumWidth(t *testing.T) {
	for _, c := range []struct {
		width int
		want  bool
	}{{detailPaneMinWidth, true}, {detailPaneMinWidth - 1, false}} {
		m := newTableModel(t, c.width)
		openSectionByName(t, m, sectionConnections)
		selectRow(m, "tracker-issues")
		view := screenOf(m)
		assertViewFits(t, view, c.width, m.termHeight)
		if has := strings.Contains(view, "Credential:") && strings.Contains(view, "Base URL:"); has != c.want {
			t.Errorf("width %d: detail pane shown = %v, want %v:\n%s", c.width, has, c.want, view)
		}
	}
}

// The pane holds the facts of the marked entry and no secret value, and text from the configuration reaches
// it without control characters.
func TestDetailPaneIsSafe(t *testing.T) {
	before := currentProfile()
	setProfile(kittui.ProfileNone)
	t.Cleanup(func() { setProfile(before) })

	m := newTableModel(t, 140)
	conn := m.cfg.Connections["tracker-issues"]
	conn.Description = "issues\x1b[31m red\x07 and\x1b]0;title\x07 more"
	conn.TargetLabels = map[string]string{"octo/test-repo": "main\x1b[2Jrepo"}
	m.cfg.Connections["tracker-issues"] = conn
	for _, s := range []section{sectionConnections, sectionCredentials, sectionServices} {
		openSectionByName(t, m, s)
		for _, name := range m.names {
			selectRow(m, name)
			view := screenOf(m)
			assertNoCanary(t, "the detail pane", view)
			for _, secretValue := range []string{"canary-tracker-token", "canary-wiki-id"} {
				if strings.Contains(view, secretValue) {
					t.Errorf("%v %q: the pane shows a secret value %q", s, name, secretValue)
				}
			}
			if strings.Contains(view, "\x1b") || strings.Contains(view, "\x07") {
				t.Errorf("%v %q: a control character reached the screen: %q", s, name, view)
			}
		}
	}
	openSectionByName(t, m, sectionConnections)
	selectRow(m, "tracker-issues")
	if view := screenOf(m); !strings.Contains(view, "issues") || !strings.Contains(view, "Description:") {
		t.Errorf("the description is missing from the pane:\n%s", view)
	}
}

// The last test of a connection shows in its pane once the model holds it.
func TestDetailPaneShowsTheLastTest(t *testing.T) {
	m := newTableModel(t, 130)
	openSectionByName(t, m, sectionConnections)
	selectRow(m, "tracker-issues")
	if view := screenOf(m); strings.Contains(view, "Last test") {
		t.Errorf("a pane shows a test nobody ran:\n%s", view)
	}
	m.testName, m.testClass = "tracker-issues", "auth"
	if view := screenOf(m); !strings.Contains(view, "Last test:") {
		t.Errorf("the pane omits the last test:\n%s", view)
	}
}

// The bar never panics and never draws a line wider than its width, at any width, focus and search state.
func TestFilterBarFitsEveryWidth(t *testing.T) {
	for _, s := range []section{sectionServices, sectionCredentials, sectionConnections} {
		for _, state := range []string{"idle", "focus", "query", "editing", "long"} {
			t.Run(fmt.Sprintf("%v/%s", s, state), func(t *testing.T) {
				t.Parallel()
				// The bar draws in any width it is given, so one editor serves every width.
				m := newTableModel(t, 100)
				openSectionByName(t, m, s)
				switch state {
				case "focus":
					press(t, m, "tab")
				case "query":
					m.list.SetFilter("wiki")
				case "editing":
					press(t, m, "/")
				case "long":
					press(t, m, "/")
					m.list.Paste(strings.Repeat("Ж", 300))
				}
				for w := 20; w <= 160; w++ {
					if got := lipgloss.Width(barLineOf(m, w)); got > w {
						t.Fatalf("section %v %s width %d: bar has %d cells", s, state, w, got)
					}
				}
			})
		}
	}
}

// Opening Connections from Overview in a 58 cell window keeps the TUI running.
func TestOverviewToConnectionsNarrowDoesNotCrash(t *testing.T) {
	m := newTableModel(t, 58)
	openSectionByName(t, m, sectionConnections)
	assertViewFits(t, screenOf(m), 58, m.termHeight)
}

// barLineOf is the filter bar of the shown list drawn in width cells, without styles.
func barLineOf(m *Model, width int) string {
	line, _ := m.barView(width)
	return ansi.Strip(line)
}

// focusedBarField is the label of the field of the filter bar that has the focus, "Filter" for the button
// that stands for the fields, and "" while the bar has no focus.
func focusedBarField(m *Model) string {
	if !m.barFocused() {
		return ""
	}
	line := barLineOf(m, m.tableRoom())
	// A field stands behind the marker and its own blank; the button carries the marker in its frame.
	at := strings.Index(line, ">  ")
	if at < 0 {
		return "Filter"
	}
	label, _, _ := strings.Cut(strings.TrimSpace(line[at+3:]), ":")
	return label
}

// barSections are the lists with a filter bar.
var barSections = []section{sectionServices, sectionCredentials, sectionConnections}

// f moves the focus to the first field of the bar from the rows and from the pane, tab leaves the last field
// for the tab line and shift+tab the first one for the rows, in each list with a bar.
func TestFilterBarFocusKeyAndEnds(t *testing.T) {
	for _, s := range barSections {
		t.Run(fmt.Sprint(s), func(t *testing.T) {
			m := newTableModel(t, 100)
			openSectionByName(t, m, s)
			press(t, m, keyLabel(actFilter))
			if m.screen != screenList || focusedBarField(m) != "Group" {
				t.Fatalf("f on the rows = screen %v bar %q, want the Group field", m.screen, focusedBarField(m))
			}
			press(t, m, keyLabel(actFilter))
			if focusedBarField(m) != "Group" {
				t.Fatalf("f on the bar moved the focus to %q", focusedBarField(m))
			}
			press(t, m, "shift+tab")
			if m.screen != screenList || m.barFocused() || !m.rowsFocused() {
				t.Fatalf("shift+tab on the first field = screen %v bar %q, want the rows", m.screen, focusedBarField(m))
			}
			press(t, m, keyLabel(actFilter))
			for range barKinds(s) {
				press(t, m, "tab")
			}
			if m.screen != screenNav || m.barFocused() {
				t.Fatalf("tab on the last field = screen %v bar %q, want the tab line", m.screen, focusedBarField(m))
			}

			wide := newTableModel(t, 140)
			openSectionByName(t, wide, s)
			press(t, wide, "tab")
			if !wide.detailFocused() {
				t.Fatal("tab on the rows did not reach the pane")
			}
			press(t, wide, keyLabel(actFilter))
			if wide.detailFocused() || focusedBarField(wide) != "Group" {
				t.Fatalf("f in the pane = pane %v bar %q, want the Group field", wide.detailFocused(), focusedBarField(wide))
			}
		})
	}
}

// The values of a field open with enter: the label is the title, the position stands in the edge, a search
// line comes first and narrows the values by part of their text in any case, the value in force carries a
// check mark, and while open every key is the list's. enter takes a value, esc closes without change, and the
// marked entry stays on its name.
func TestFilterBarValuesSearchShowPositionAndCheck(t *testing.T) {
	for _, s := range barSections {
		t.Run(fmt.Sprint(s), func(t *testing.T) {
			m := newTableModel(t, 100)
			openSectionByName(t, m, s)
			name := shownEntries(m)[len(shownEntries(m))-1]
			selectRow(m, name)
			press(t, m, keyLabel(actFilter), "enter")
			if !m.barValuesOpen() {
				t.Fatal("enter on the Group field opened no values")
			}
			view := ansi.Strip(screenOf(m))
			for _, want := range []string{"═ Group ═", "1/2", "Type to sea", "✓ Provider", "  None"} {
				if !strings.Contains(view, want) {
					t.Fatalf("the values lack %q:\n%s", want, view)
				}
			}
			m.Update(tea.PasteMsg{Content: "n\x1bO"})
			view = ansi.Strip(screenOf(m))
			if !strings.Contains(view, "None") || strings.Contains(view, "✓ Provider") {
				t.Fatalf("the search \"nO\" does not narrow to None:\n%s", view)
			}
			// While the values are open, letters and digits are their search, never a key of the screen.
			typeText(t, m, "q?:3")
			if m.quitting || m.screen != screenList || m.section != s || !m.barValuesOpen() {
				t.Fatalf("a letter acted beside the values: quitting %v screen %v section %v", m.quitting, m.screen, m.section)
			}
			press(t, m, "esc")
			if m.barOpen() || m.opts(s).flat || focusedBarField(m) != "Group" {
				t.Fatalf("esc did not close the values unchanged: open %v flat %v", m.barOpen(), m.opts(s).flat)
			}
			press(t, m, "enter", "down", "enter")
			if m.barOpen() || !m.opts(s).flat || listGrouped(m) {
				t.Fatalf("enter on None: open %v flat %v grouped %v", m.barOpen(), m.opts(s).flat, listGrouped(m))
			}
			if got, _ := m.marked(); got != name {
				t.Errorf("the marked entry moved from %q to %q", name, got)
			}
			press(t, m, "down")
			if !m.rowsFocused() {
				t.Error("down on a closed field did not lead to the rows")
			}
		})
	}
}

// Group, State and Sort change the list in every list with a bar, and none touches the configuration.
func TestFilterBarFieldsShapeEachList(t *testing.T) {
	for _, s := range barSections {
		t.Run(fmt.Sprint(s), func(t *testing.T) {
			m := newTableModel(t, 100)
			before := configBytes(m)
			openSectionByName(t, m, s)
			all := slices.Sorted(slices.Values(shownEntries(m)))
			press(t, m, keyLabel(actFilter), "right")
			if listGrouped(m) || !reflect.DeepEqual(shownEntries(m), all) {
				t.Fatalf("Group None: grouped %v entries %v", listGrouped(m), shownEntries(m))
			}
			for range barKinds(s)[1:] {
				press(t, m, "tab")
			}
			press(t, m, "right")
			of := m.providerOf(s)
			got := shownEntries(m)
			if len(got) != len(all) || !slices.IsSortedFunc(got, func(a, b string) int { return strings.Compare(of(a), of(b)) }) {
				t.Fatalf("Sort Provider = %v, want every entry by provider", got)
			}
			if after := configBytes(m); after != before || m.dirty() {
				t.Error("the bar changed the configuration")
			}
		})
	}
}

// A list without any entry has no bar, and f and the search key do nothing there.
func TestFilterBarIsLeftOutOfAnEmptyList(t *testing.T) {
	for _, s := range barSections {
		t.Run(fmt.Sprint(s), func(t *testing.T) {
			m := newTableModel(t, 100)
			m.cfg.Services, m.cfg.Credentials, m.cfg.Connections = nil, nil, nil
			openSectionByName(t, m, s)
			if m.listBarShown() || strings.Contains(ansi.Strip(screenOf(m)), "Group:") {
				t.Fatalf("an empty list shows a bar:\n%s", ansi.Strip(screenOf(m)))
			}
			press(t, m, keyLabel(actFilter), keyLabel(actSearch))
			if m.barFocused() || m.list.Searching() || m.screen != screenList {
				t.Errorf("f or the search key acted on an empty list: bar %v search %v", m.barFocused(), m.list.Searching())
			}
		})
	}
}

// A state that hides every connection keeps the bar, which sets it back, and the list says why it is empty.
func TestFilterBarThatHidesEverythingCanBeSetBack(t *testing.T) {
	m := newTableModel(t, 100)
	openSectionByName(t, m, sectionConnections)
	press(t, m, keyLabel(actFilter), "tab", "left")
	if m.opts(sectionConnections).state != 2 || len(shownEntries(m)) != 0 {
		t.Fatalf("State Inactive: state %d entries %v", m.opts(sectionConnections).state, shownEntries(m))
	}
	view := ansi.Strip(screenOf(m))
	if !strings.Contains(view, "State: Inactive") || !strings.Contains(view, "No entry matches the filter bar") {
		t.Fatalf("the empty list lost its bar or its reason:\n%s", view)
	}
	press(t, m, "down", keyLabel(actFilter), "tab", "right")
	if m.opts(sectionConnections).state != 0 || len(shownEntries(m)) != 3 {
		t.Fatalf("State All again: state %d entries %v", m.opts(sectionConnections).state, shownEntries(m))
	}
}

// The search field of the list stands at the right end of the bar: idle with its key, typed and kept with the
// text, and it reads only the drawn text of a row, never a secret value.
func TestFilterBarSearchFieldReadsTheShownText(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		m := newTableModel(t, 100)
		openSectionByName(t, m, sectionConnections)
		idle := translator(false).Lookup(kittui.TextSearchKey, keyLabel(actSearch))
		line := barLineOf(m, m.tableRoom())
		if !strings.HasSuffix(strings.TrimRight(line, " "), strings.TrimRight(idle, " ")) && !strings.Contains(line, idle) {
			t.Fatalf("the idle bar shows no search field: %q", line)
		}
		at := strings.Index(line, idle)
		if w := lipgloss.Width(line[at:]); w < 14 || w > 30 {
			t.Errorf("the search field is %d cells wide, want 14 to 30: %q", w, line)
		}
		press(t, m, keyLabel(actSearch))
		typeText(t, m, "triage")
		if line := barLineOf(m, m.tableRoom()); !strings.Contains(line, "/ triage") {
			t.Fatalf("the typed search is not in the bar: %q", line)
		}
		press(t, m, "enter")
		if got := shownEntries(m); !reflect.DeepEqual(got, []string{"tracker-triage"}) || !strings.Contains(barLineOf(m, m.tableRoom()), "/ triage") {
			t.Fatalf("the kept search shows %v, bar %q", got, barLineOf(m, m.tableRoom()))
		}
		press(t, m, "esc", keyLabel(actSearch))
		typeText(t, m, "canary-tracker-token")
		if got := shownEntries(m); len(got) != 0 {
			t.Errorf("a secret value matches %v", got)
		}
	})
}

// narrowBar is a Connections list in a terminal of width cells whose bar shows the filter button.
func narrowBar(t *testing.T, width int) *Model {
	t.Helper()
	m := newTableModel(t, width)
	openSectionByName(t, m, sectionConnections)
	if width > 44 {
		// The fields fit beside an idle search; a kept search leaves no room for them.
		m.list.SetFilter("tracker")
	}
	return m
}

// In a narrow terminal one button with the number of fields off their first value stands for the fields, with
// the search field beside it. enter opens a modal with every field, which takes every key; enter there takes
// all changes and esc drops them all.
func TestFilterBarButtonAndModalOnANarrowTerminal(t *testing.T) {
	for _, width := range []int{44, 60} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := narrowBar(t, width)
			if line := barLineOf(m, m.tableRoom()); !strings.Contains(line, "Filter (0)") || strings.Contains(line, "Group") {
				t.Fatalf("the narrow bar = %q, want the button", line)
			}
			press(t, m, keyLabel(actFilter))
			if focusedBarField(m) != "Filter" {
				t.Fatalf("f = bar %q, want the button", focusedBarField(m))
			}
			press(t, m, "enter")
			if !m.barOpen() {
				t.Fatal("enter on the button opened no modal")
			}
			view := ansi.Strip(m.render())
			for _, want := range []string{"═ Filter ═", "Group: Provider", "State: All", "Sort : Name", "enter apply", "esc discard"} {
				if !strings.Contains(view, want) {
					t.Fatalf("the modal lacks %q:\n%s", want, view)
				}
			}
			typeText(t, m, "q?:3")
			if m.quitting || m.screen != screenList || m.section != sectionConnections || !m.barOpen() {
				t.Fatalf("a key acted beside the modal: quitting %v screen %v section %v", m.quitting, m.screen, m.section)
			}
			press(t, m, "down", "right", "down", "right", "esc")
			if o := m.opts(sectionConnections); m.barOpen() || o.state != 0 || o.byProvider {
				t.Fatalf("esc kept changes: open %v state %d by provider %v", m.barOpen(), o.state, o.byProvider)
			}
			press(t, m, "enter", "down", "right", "down", "right", "enter")
			if o := m.opts(sectionConnections); m.barOpen() || o.state != 1 || !o.byProvider || o.flat {
				t.Fatalf("enter took state %d by provider %v flat %v, want Active by provider", o.state, o.byProvider, o.flat)
			}
			if line := barLineOf(m, m.tableRoom()); !strings.Contains(line, "Filter (2)") {
				t.Errorf("the button = %q, want two fields off their first value", line)
			}
			press(t, m, "tab")
			if m.screen != screenNav {
				t.Errorf("tab on the button = screen %v, want the tab line", m.screen)
			}
		})
	}
}

// Where the modal has no room over the screen it fills the workspace, its keys in the footer, and stays usable.
func TestFilterBarModalInTheWorkspace(t *testing.T) {
	m := newTableModel(t, minimumWidth)
	m.Update(tea.WindowSizeMsg{Width: minimumWidth, Height: minimumHeight})
	openSectionByName(t, m, sectionConnections)
	press(t, m, keyLabel(actFilter), "enter")
	if !m.barOpen() || m.barValuesOpen() {
		t.Fatalf("enter on the button opened no modal:\n%s", ansi.Strip(m.render()))
	}
	if _, _, over := m.barModalOverlay(""); over {
		t.Fatal("the modal fits over a terminal of the least size")
	}
	view := m.render()
	assertViewFits(t, view, minimumWidth, minimumHeight)
	if plain := ansi.Strip(view); !strings.Contains(plain, "Sort : Name") || !strings.Contains(plain, "apply") {
		t.Fatalf("the modal is not usable at the least size:\n%s", plain)
	}
	press(t, m, "down", "down", "right", "enter")
	if !m.opts(sectionConnections).byProvider {
		t.Error("Provider was not taken as the sort")
	}
}

// Without colour the focus reads from the marker and the value in force from its check mark, and neither the
// bar nor its values carry a colour sequence.
func TestFilterBarReadsWithoutColour(t *testing.T) {
	before := currentProfile()
	prior := styles
	t.Cleanup(func() { setProfile(before); setTheme(prior) })
	setProfile(kittui.ProfileMono)
	applyTheme(kittui.ThemeTerminal)

	m := newTableModel(t, 100)
	openSectionByName(t, m, sectionConnections)
	press(t, m, keyLabel(actFilter), "enter")
	view := m.render()
	if hasColour(view) {
		t.Errorf("the view carries a colour under NO_COLOR:\n%q", view)
	}
	if plain := ansi.Strip(view); !strings.Contains(plain, ">  Group: Provider ▾") || !strings.Contains(plain, "✓ Provider") {
		t.Errorf("the focus is not readable without colour:\n%s", plain)
	}
}
