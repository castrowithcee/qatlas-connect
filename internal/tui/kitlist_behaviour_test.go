package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// mainSections are the sections whose screen is a list of entries.
var mainSections = []section{sectionServices, sectionCredentials, sectionConnections, sectionDefaults,
	sectionApprovals, sectionTokens}

// mainListModel is the table fixture of width by height on the list of s. Approvals and Tokens hold no entry
// without a vault, so they get two named rows.
func mainListModel(t *testing.T, s section, width, height int) *Model {
	t.Helper()
	m := sizedModel(newTableModel(t, width), width, height)
	openSectionByName(t, m, s)
	if s == sectionApprovals || s == sectionTokens {
		setNames(m, "alpha-entry", "beta-entry-with-a-longer-name")
		m.screen = screenList
	}
	return m
}

// prose is a view as one line of words, the frame left out, so a wrapped sentence reads whole.
func prose(view string) string {
	return flatText(strings.NewReplacer("║", " ", "│", " ", "#", " ").Replace(view))
}

// The first column of every list is never cut, at 80 and at 150 cells, and where the room is short the
// optional columns go from the right: an optional column only goes after every optional one right of it.
func TestMainListsFitTheirColumns(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {150, 42}} {
		for _, s := range mainSections {
			t.Run(fmt.Sprintf("%v/%dx%d", s, size[0], size[1]), func(t *testing.T) {
				m := mainListModel(t, s, size[0], size[1])
				header, rows := tableLines(t, m)
				cols := sectionColumns[s]
				shown := make([]bool, len(cols))
				for i, c := range cols {
					shown[i] = strings.Contains(header, string(c.Title))
					if !shown[i] && !c.Optional {
						t.Errorf("the column %s is missing:\n%s", c.Title, screenOf(m))
					}
				}
				for i := range cols {
					for j := i + 1; j < len(cols); j++ {
						if cols[i].Optional && cols[j].Optional && !shown[i] && shown[j] {
							t.Errorf("%s is left out before %s right of it:\n%s", cols[i].Title, cols[j].Title, screenOf(m))
						}
					}
				}
				entries := shownEntries(m)
				if len(rows) == 0 || len(rows) != len(entries) {
					t.Fatalf("%d rows for %d entries:\n%s", len(rows), len(entries), screenOf(m))
				}
				for i, row := range rows {
					if got := cellAt(header, row, string(cols[0].Title)); got != m.cells(entries[i])[0] {
						t.Errorf("the first cell %q is cut from %q:\n%s", got, m.cells(entries[i])[0], screenOf(m))
					}
				}
			})
		}
	}
}

// The selection follows the name: through a typed filter and its end, a rename and a new reading of the
// entries.
func TestTheSelectionFollowsTheName(t *testing.T) {
	m := sizedModel(groupModel(t), 150, 42)
	openSectionByName(t, m, sectionConnections)
	selectRow(m, "tracker-triage")
	want := func(name, when string) {
		t.Helper()
		if got, _ := m.selected(); got != name {
			t.Errorf("%s: selected %q, want %q", when, got, name)
		}
	}
	press(t, m, "/")
	typeText(t, m, "tracker")
	want("tracker-triage", "a typed filter")
	press(t, m, "esc")
	want("tracker-triage", "the cleared filter")
	conn := m.cfg.Connections["tracker-triage"]
	delete(m.cfg.Connections, "tracker-triage")
	m.cfg.Connections["tracker-sorting"] = conn
	m.returnToList("tracker-sorting")
	want("tracker-sorting", "the rename")
	m.fillList()
	screenOf(m)
	want("tracker-sorting", "a new reading")
}

// Enter folds and unfolds a group row, left folds the group of an entry and marks its row, right unfolds it;
// while a filter is set the groups stay open and the keys change nothing.
func TestGroupsFoldWithEnterLeftAndRight(t *testing.T) {
	m := sizedModel(groupModel(t), 150, 42)
	openSectionByName(t, m, sectionConnections)
	selectRow(m, groupRow("tracker"))
	press(t, m, "enter")
	if !groupFolded(m, "tracker") || m.screen != screenList {
		t.Fatalf("enter on the group row: folded %v, screen %v", groupFolded(m, "tracker"), m.screen)
	}
	press(t, m, "enter")
	if groupFolded(m, "tracker") {
		t.Fatal("enter on the folded group row does not unfold it")
	}
	selectRow(m, "tracker-issues")
	press(t, m, "left")
	if name, _ := m.marked(); !groupFolded(m, "tracker") || name != groupRow("tracker") {
		t.Fatalf("left on an entry: folded %v, marked %q", groupFolded(m, "tracker"), name)
	}
	press(t, m, "right")
	if groupFolded(m, "tracker") {
		t.Fatal("right on the folded group row does not unfold it")
	}
	press(t, m, "/")
	typeText(t, m, "tracker")
	press(t, m, "enter")
	selectRow(m, groupRow("tracker"))
	before := listRowsOf(m)
	for _, key := range []string{"enter", "left", "right"} {
		press(t, m, key)
		if got := listRowsOf(m); !slices.Equal(got, before) || m.screen != screenList {
			t.Errorf("%s under a filter changed the rows to %q (screen %v), want %q", key, got, m.screen, before)
		}
	}
}

// An entry of a folded group that is selected by name unfolds its group.
func TestSelectingAnEntryOfAFoldedGroupUnfoldsIt(t *testing.T) {
	m := sizedModel(groupModel(t), 150, 42)
	openSectionByName(t, m, sectionConnections)
	selectRow(m, groupRow("tracker"))
	press(t, m, "enter")
	m.returnToList("tracker-issues")
	if name, _ := m.selected(); name != "tracker-issues" || groupFolded(m, "tracker") {
		t.Errorf("selected %q, folded %v, want the entry in its unfolded group", name, groupFolded(m, "tracker"))
	}
}

// The filter finds an entry by each of its cells, by its provider and by every target of a connection, even
// where the column shows a label or a count instead.
func TestTheFilterFindsEveryCellTheProviderAndTheTargets(t *testing.T) {
	m := sizedModel(groupModel(t), 150, 42)
	finds := func(query, name string) {
		t.Helper()
		m.list.SetFilter(query)
		if !slices.Contains(shownEntries(m), name) {
			t.Errorf("%v: the filter %q does not find %q, shows %q", m.section, query, name, shownEntries(m))
		}
		m.list.SetFilter("")
	}
	for _, s := range []section{sectionServices, sectionCredentials, sectionConnections, sectionDefaults} {
		openSectionByName(t, m, s)
		for _, name := range m.names {
			for _, cell := range m.cells(name) {
				if cell = strings.TrimSpace(cell); cell != "" && cell != "-" {
					finds(cell, name)
				}
			}
		}
	}
	openSectionByName(t, m, sectionServices)
	finds("tracker", "tracker-main")
	openSectionByName(t, m, sectionCredentials)
	finds("bookstack", "wiki-reader")
	openSectionByName(t, m, sectionConnections)
	finds("bookstack", "wiki-docs")
	for _, target := range []string{"octo/a", "octo/b", "octo/c"} {
		finds(target, "tracker-triage")
	}
}

// No cell and no search text of any list holds a secret value, and the filter finds nothing by one.
func TestNoListHoldsASecretValue(t *testing.T) {
	m := sizedModel(newTableModel(t, 150), 150, 42)
	for _, s := range mainSections {
		openSectionByName(t, m, s)
		for _, name := range m.names {
			if text := strings.Join(m.cells(name), " ") + " " + m.describe(name); strings.Contains(text, "canary") {
				t.Errorf("%v: %q shows a secret value: %q", s, name, text)
			}
		}
		m.list.SetFilter("canary")
		if got := shownEntries(m); len(got) > 0 {
			t.Errorf("%v: the filter finds %q by a secret value", s, got)
		}
		m.list.SetFilter("")
		if view := screenOf(m); strings.Contains(view, "canary") {
			t.Errorf("%v: the list shows a secret value:\n%s", s, view)
		}
	}
}

// Each state of an empty list names itself and its next step, a filter bar that hides every entry says so,
// and a typed filter without match names the key that clears it.
func TestEveryEmptyStateHasItsSentence(t *testing.T) {
	m, _, _ := newModel(t)
	sizedModel(m, 150, 42)
	for s, id := range map[section]textID{sectionServices: tFormNoServicesYetPressN,
		sectionCredentials: tFormNoCredentialsYetPressN} {
		openSectionByName(t, m, s)
		if view := prose(screenOf(m)); !strings.Contains(view, flatText(tr(id))) {
			t.Errorf("%v: the empty list lacks %q:\n%s", s, tr(id), view)
		}
	}
	for _, s := range []section{sectionConnections, sectionDefaults, sectionApprovals, sectionTokens} {
		openSectionByName(t, m, s)
		if want := flatText(m.emptyHelp()); !strings.Contains(prose(screenOf(m)), want) {
			t.Errorf("%v: the empty list lacks %q:\n%s", s, want, screenOf(m))
		}
	}

	g := sizedModel(groupModel(t), 150, 42)
	openSectionByName(t, g, sectionConnections)
	g.opts(sectionConnections).state = 2
	g.relist()
	if view := prose(screenOf(g)); !strings.Contains(view, flatText(tr(tFormNoEntryMatchesTheFilter))) {
		t.Errorf("a bar that hides every entry does not say so:\n%s", view)
	}
	g.opts(sectionConnections).state = 0
	g.relist()
	press(t, g, "/")
	typeText(t, g, "zzz")
	want := flatText(translator(false).Lookup(kittui.TextListNoMatch, "zzz", keyLabel(actBack)))
	if view := prose(screenOf(g)); !strings.Contains(view, want) {
		t.Errorf("a filter without match lacks %q:\n%s", want, view)
	}
}

// The column names stand in bold capitals over a rule, which an ASCII terminal draws with '-'.
func TestTheColumnNamesStandOverARule(t *testing.T) {
	restoreRenderer(t)
	lookOf(kittui.ProfileTrueColor)
	m := sizedModel(groupModel(t), 150, 42)
	openSectionByName(t, m, sectionServices)
	lines := strings.Split(m.render(), "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(ansi.Strip(l), "NAME") })
	if at < 0 || !strings.Contains(lines[at], "\x1b[1") || !strings.Contains(ansi.Strip(lines[at+1]), "───") {
		t.Fatalf("no bold column names over a rule:\n%q", lines[max(at, 0):max(at+2, 0)])
	}
	withoutUTF8(t, "c")
	lookOf(kittui.ProfileNone)
	view := screenOf(m)
	if !strings.Contains(view, "      NAME") || !strings.Contains(view, "      -----") {
		t.Errorf("the ASCII rule is missing:\n%s", view)
	}
}

// A group row shows the fold glyph, the category mark in the colour the kit gives the provider, the provider
// and the number of its entries.
func TestAGroupRowCarriesTheCategoryMarkOfItsProvider(t *testing.T) {
	restoreRenderer(t)
	lookOf(kittui.ProfileTrueColor)
	m := sizedModel(groupModel(t), 150, 42)
	openSectionByName(t, m, sectionConnections)
	if view := ansi.Strip(screenOf(m)); !strings.Contains(view, "▾ ■ tracker (2)") {
		t.Errorf("the group row lacks its parts:\n%s", view)
	}
	if !strings.Contains(m.render(), styles.Styles.CategoryMark("tracker")) {
		t.Error("the group row lacks the category mark of its provider")
	}
}

// The search field of a list without filter bar is the kit's, a line of its own in the table that opens with
// the search key, and no "filter:" label is left.
func TestTheSearchFieldIsTheListsOwn(t *testing.T) {
	m := mainListModel(t, sectionDefaults, 150, 42)
	press(t, m, "/")
	typeText(t, m, "book")
	view := screenOf(m)
	header, _ := tableLines(t, m)
	lines := strings.Split(view, "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, header) })
	if at < 1 || !strings.Contains(lines[at-1], keyLabel(actSearch)+" book") || strings.Contains(view, "filter: ") {
		t.Errorf("the search field is not the list's line above the column names:\n%s", view)
	}
}

// A control character in a cell never reaches the screen: names and descriptions are cleaned before the
// list sees them, and the list shows any other one escaped.
func TestControlCharactersInACellAreEscaped(t *testing.T) {
	restoreRenderer(t)
	lookOf(kittui.ProfileNone)
	m := sizedModel(groupModel(t), 150, 42)
	conn := m.cfg.Connections["wiki-docs"]
	conn.Description = "pages\x1b[31m red\x07 end"
	m.cfg.Connections["wiki-docs"] = conn
	m.cfg.Defaults.Connections["bookstack"] = "wiki\x07docs"
	for _, s := range []section{sectionConnections, sectionDefaults} {
		openSectionByName(t, m, s)
		view := m.render()
		if strings.ContainsAny(view, "\x1b\x07") {
			t.Errorf("%v: a control character reaches the screen:\n%q", s, view)
		}
	}
	if view := screenOf(m); !strings.Contains(view, kittui.Escape("wiki\x07docs")) {
		t.Errorf("the list does not escape the control character:\n%s", view)
	}
}

// Every list draws at the smallest, the compact, the usual and a large terminal, with and without the detail
// pane, within the terminal and with its selection shown.
func TestMainListsDrawAtEverySize(t *testing.T) {
	for _, size := range [][2]int{{40, 15}, {60, 16}, {80, 24}, {150, 42}} {
		for _, s := range mainSections {
			for _, flat := range []bool{false, true} {
				if flat && !groupedSection(s) {
					continue
				}
				t.Run(fmt.Sprintf("%v/%dx%d/flat=%v", s, size[0], size[1], flat), func(t *testing.T) {
					m := mainListModel(t, s, size[0], size[1])
					if flat {
						// The pane stands beside a grouped list from its width on; a flat one keeps it too, so
						// the narrow sizes are those without it.
						m.opts(s).flat = true
						m.relist()
					}
					view := m.render()
					assertViewFits(t, view, size[0], size[1])
					if size[0] >= detailPaneMinWidth && groupedSection(s) && m.paneWidth() == 0 {
						t.Errorf("no detail pane at %dx%d", size[0], size[1])
					}
					if !strings.Contains(screenOf(m), selectionMark()+" ") {
						t.Errorf("no row is marked:\n%s", screenOf(m))
					}
				})
			}
		}
	}
}

// The notes about the entries give way only where the list has no room for a row: at 80x24 the idle warning
// and the hint about missing descriptions stand under the list.
func TestTheNotesUnderTheListStayAtTheUsualSize(t *testing.T) {
	m := sizedModel(newTableModel(t, 80), 80, 24)
	openSectionByName(t, m, sectionConnections)
	view := prose(screenOf(m))
	for _, want := range []string{"offers no tool", flatText(tr(tFormUndescribedHint))} {
		if !strings.Contains(view, want) {
			t.Errorf("the note %q is missing at 80x24:\n%s", want, screenOf(m))
		}
	}
}

// A notch of the wheel over the rows moves the selection as down does and takes no focus; over the detail
// pane it moves no row.
func TestTheWheelMovesTheSelectionOverTheRowsOnly(t *testing.T) {
	build := func() *Model {
		m := sizedModel(groupModel(t), 150, 42)
		openSectionByName(t, m, sectionConnections)
		m.render()
		return m
	}
	wheeled, pressed := build(), build()
	// The pane names the marked entry, so the pointer goes to another row.
	x, y := cellOf(t, wheeled, "tracker-issues")
	_, cmd := wheeled.Update(mouseEvent(x, y, tea.MouseWheelDown))
	deliver(wheeled, cmd)
	press(t, pressed, "down")
	if a, b := clickState(wheeled), clickState(pressed); a != b {
		t.Errorf("the wheel over the rows does not act as down:\nwheel:\n%s\nkey:\n%s", a, b)
	}
	over := build()
	before := focusState(over)
	selected, _ := over.marked()
	_, cmd = over.Update(mouseEvent(over.detailPane.x0+2, over.detailPane.y0+2, tea.MouseWheelDown))
	deliver(over, cmd)
	if now, _ := over.marked(); now != selected || focusState(over) != before {
		t.Errorf("the wheel over the pane moved the row to %q or the focus to %q", now, focusState(over))
	}
}
