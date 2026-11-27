package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// exportModelWithNames is the plain export model with a service and a credential more, and the export open.
func exportModelWithNames(t *testing.T) *exportViews {
	t.Helper()
	v, _ := plainExportModel(t)
	mustNoError(t, v.m.cfg.SetService("tracker", config.Service{Provider: "bookstack", BaseURL: "https://tracker.example.invalid"}))
	mustNoError(t, v.m.store.Save(v.m.cfg))
	syncRevision(t, v.m)
	v.startExport("")
	return v
}

func TestExportSelectionIsOnlyWhatIsTicked(t *testing.T) {
	v := exportModelWithNames(t)
	e := v.m.export
	// The first entry is marked: defaults. Tick it and the connection docs.
	v.pump(" ", "down", "down", " ")
	sel := e.selection()
	if !sel.Defaults || sel.ProviderNotes || sel.All || !slices.Equal(sel.Connections, []string{"docs"}) ||
		len(sel.Services) != 0 || len(sel.Credentials) != 0 {
		t.Fatalf("selection %+v, want defaults and the connection docs only", sel)
	}
	if e.ticked() != 2 {
		t.Fatalf("ticked %d, want 2", e.ticked())
	}
}

func TestExportAllRowAndTicksActOnTheShownEntriesOnly(t *testing.T) {
	v := exportModelWithNames(t)
	e := v.m.export
	v.pump("/")
	v.type_("tracker")
	v.pump("enter")
	if got := e.list.Shown(); len(got) != 1 || e.entries[atoi(t, got[0])] != exportSvcPref+"tracker" {
		t.Fatalf("shown %v, want the service tracker only", got)
	}
	// The row of all ticks what the filter lets through, and nothing hidden.
	v.pump("up", " ")
	sel := e.selection()
	if !slices.Equal(sel.Services, []string{"tracker"}) || sel.Defaults || sel.ProviderNotes || len(sel.Connections) != 0 {
		t.Fatalf("selection %+v after the row of all under a filter, want the service tracker only", sel)
	}
	if pos := e.list.Position(); !strings.Contains(pos, "(") {
		t.Fatalf("position %q, want the count of the shown entries with the total", pos)
	}
}

func TestExportEscClearsTheFilterThenLeavesAndAsksWhenSomethingIsTicked(t *testing.T) {
	v := exportModelWithNames(t)
	m := v.m
	v.pump("/")
	v.type_("tracker")
	v.pump("esc")
	if m.export == nil || m.export.step != exportSelect || m.export.list.Query() != "" || m.export.list.Searching() {
		t.Fatalf("first esc: export %v, query %q; want the filter cleared and the selection kept", m.export != nil, m.export.list.Query())
	}
	v.pump(" ", "esc")
	if m.screen != screenLeave {
		t.Fatalf("esc with a tick = screen %v, want the leave question", m.screen)
	}
}

func TestExportRowsAndCountsShowNoSecretValue(t *testing.T) {
	v, _ := vaultExportModel(t)
	v.startExport(testExportVaultPass)
	e := v.m.export
	v.pump("up", " ")
	v.views = append(v.views, e.list.Position(), strings.Join(exportModeRows(), "\n"))
	v.pump("enter", "down")
	if e.step != exportMode {
		t.Fatalf("step %v, want the mode question", e.step)
	}
	v.assertHidden(testExportCanary, testExportVaultPass)
}

func TestExportNamesWithControlCharactersAreEscapedAndNeverIDs(t *testing.T) {
	v, _ := plainExportModel(t)
	const evil = "x\x1b]0;owned\x07\x1b[2Jy"
	mustNoError(t, v.m.cfg.SetService(evil, config.Service{Provider: "bookstack", BaseURL: "https://evil.example.invalid"}))
	v.startExport("")
	e := v.m.export
	for _, id := range e.list.Shown() {
		if _, err := strconv.Atoi(string(id)); err != nil {
			t.Fatalf("the entry ID %q is no index", id)
		}
	}
	view := v.m.render()
	for _, bad := range []string{"\x07", "\x1b]", "\x1b[2J"} {
		if strings.Contains(view, bad) {
			t.Fatalf("the view holds the control sequence %q:\n%s", bad, view)
		}
	}
	if !strings.Contains(ansi.Strip(view), "x") || strings.ContainsRune(ansi.Strip(view), 0x1b) {
		t.Fatalf("the name does not stand as filtered text:\n%s", ansi.Strip(view))
	}
	// Ticking the entry still names the real service in the selection.
	e.list.Select(choiceID(slices.Index(e.entries, exportSvcPref+evil)))
	v.pump(" ")
	if sel := e.selection(); !slices.Equal(sel.Services, []string{evil}) {
		t.Fatalf("selection %+v, want the service by its real name", sel)
	}
}

func TestFindingsOpenThePlanOfTheMarkedFindingAndNeverActOnAnAmbiguousOne(t *testing.T) {
	s := newFindScene(t)
	s.renameByHand(t, "credential", "reader", "reader2")
	m := s.open(t)
	openSectionByName(t, m, sectionCredentials)
	m.openFindings()
	items := append([]renameFinding{{kind: "connection", entry: "x", from: "x", to: "y", ambiguous: true}}, m.findings.items...)
	list, err := m.findingsListFor(items, 0)
	mustNoError(t, err)
	m.findings.items, m.findings.list = items, list

	press(t, m, "enter")
	if m.screen != screenFindings || m.rename != nil || m.status != tr(tFindAmbiguousStatus) {
		t.Fatalf("enter on an ambiguous finding: screen %v rename %v status %q, want no plan", m.screen, m.rename != nil, m.status)
	}
	press(t, m, "B")
	if m.screen != screenFindings || m.rename != nil {
		t.Fatalf("put back on an ambiguous finding: screen %v rename %v, want no plan", m.screen, m.rename != nil)
	}
	press(t, m, "down", "enter")
	if m.screen != screenRename || m.rename == nil || m.rename.old != "reader" {
		t.Fatalf("enter on the second finding: screen %v rename %+v, want the plan of reader", m.screen, m.rename)
	}
}

func TestFindingsShowNamesWithoutControlCharacters(t *testing.T) {
	s := newFindScene(t)
	s.renameByHand(t, "credential", "reader", "reader2")
	m := s.open(t)
	openSectionByName(t, m, sectionCredentials)
	m.openFindings()
	const evil = "r\x1b[2J\x07z"
	items := []renameFinding{{kind: "credential", entry: evil, from: evil, to: evil + "2", leftover: true, carried: []string{evil}}}
	list, err := m.findingsListFor(items, 0)
	mustNoError(t, err)
	m.findings.items, m.findings.list = items, list
	view := m.render()
	for _, bad := range []string{"\x07", "\x1b[2J"} {
		if strings.Contains(view, bad) {
			t.Fatalf("the view holds the control sequence %q:\n%s", bad, view)
		}
	}
	if id, ok := m.findings.list.Selected(); !ok || id != "0" {
		t.Fatalf("the finding is marked by %q, want its index", id)
	}
}

func TestWheelOverTheExportRowsActsAsDown(t *testing.T) {
	build := func() *Model {
		v := exportModelWithNames(t)
		v.m.render()
		return v.m
	}
	pressed, wheeled := build(), build()
	press(t, pressed, firstKey(t, actDown))
	x, y := cellOf(t, wheeled, "tracker")
	wheelEvent(t, wheeled, x, y, tea.MouseWheelDown)
	if want, got := clickState(pressed), clickState(wheeled); want != got {
		t.Errorf("a notch of the wheel does not act as down:\nkey:\n%s\nwheel:\n%s", want, got)
	}
}
