package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/helptopics"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// helpTabClick clicks the tab of the help topic at index i and checks that the topic switched and starts at
// its top. The tabs stand in the row of the title or the one under it, a tab cut at the right end by its
// first letters.
func helpTabClick(t *testing.T, m *Model, i int) {
	t.Helper()
	m.render()
	press(t, m, "down")
	if helpOffset(t, m) != 1 {
		t.Fatal("down did not scroll the help")
	}
	name := helptopics.All()[i].Name
	lines := frameLines(m)
	x, y := -1, -1
	for row, line := range lines {
		if !strings.Contains(line, "Help") {
			continue
		}
		for _, r := range []int{row, row + 1} {
			if at := strings.Index(lines[r], name[:min(5, len(name))]); at >= 0 {
				x, y = ansi.StringWidth(lines[r][:at]), r
				break
			}
		}
		break
	}
	if y < 0 {
		t.Fatalf("no tab shows the topic %q:\n%s", name, strings.Join(lines, "\n"))
	}
	clickAt(t, m, x, y)
	if !helpOpen(m) || helpTopicIndex(t, m) != i || helpOffset(t, m) != 0 {
		t.Fatalf("click on the tab %q: help open %v, want topic %d from its top", name, helpOpen(m), i)
	}
	if view := strings.Join(frameLines(m), "\n"); !strings.Contains(view, "["+name[:min(5, len(name))]) {
		t.Errorf("the topic %q is not the active tab:\n%s", name, view)
	}
}

func TestClickOnAHelpTopicTabSwitchesTheTopicInTheOverlayAndTheWorkspace(t *testing.T) {
	last := len(helptopics.All()) - 1
	for _, size := range []struct {
		name  string
		w, h  int
		order []int
	}{{"overlay", 120, 40, []int{1, last, 0}}, {"workspace", 43, 16, []int{1, 2, 1}}} {
		t.Run(size.name, func(t *testing.T) {
			m := sizedModel(groupModel(t), size.w, size.h)
			m.prefs.Icons = kitprefs.IconsNone
			m.openHelp()
			view := strings.Join(frameLines(m), "\n")
			if framed := strings.Contains(view, "═ Help ") || strings.Contains(view, "─ Help "); framed != (size.name == "overlay") {
				t.Fatalf("help framed over the screen = %v at %dx%d", framed, size.w, size.h)
			}
			for _, i := range size.order {
				helpTabClick(t, m, i)
			}
		})
	}
}

// A click on a filter of the bar opens its values, a second one closes them, and a click on a value takes it.
func TestClickOnAFilterOpensItsValuesAndAClickOnAValueTakesIt(t *testing.T) {
	f := newLogFixture(t)
	f.append("mcp", "codex", "wiki.pages.get", "wiki", "success")
	f.append("mcp", "claude-code", "wiki.pages.create", "wiki", "policy-denied")
	m := sizedModel(f.model(true, f.localToday()), 180, 40)
	m.prefs.Icons = kitprefs.IconsNone
	m.render()
	clickOn(t, m, "Client: all")
	if m.logs.focus != logFocusBar || !m.logs.bar.Open() {
		t.Fatalf("a click on the client filter did not open its values:\n%s", m.render())
	}
	clickOn(t, m, "Client: all")
	if m.logs.bar.Open() || m.logs.filters[logFilterClient] != "" {
		t.Fatalf("a second click did not close the values unchanged: filters %v", m.logs.filters)
	}
	clickOn(t, m, "Client: all")
	clickOn(t, m, "claude-code")
	if m.logs.bar.Open() || m.logs.filters[logFilterClient] != "claude-code" {
		t.Fatalf("a click on a value did not take it: filters %v", m.logs.filters)
	}
}

func TestClickOnATargetOfThePickerMarksItAndASecondClickTakesIt(t *testing.T) {
	src := &listSource{list: pickPage(group("a"), group("b"), group("c"))}
	m := pickerModel(t, src)
	m.prefs.Icons = kitprefs.IconsNone
	openPicker(t, m)
	m.render()
	clickOn(t, m, "Group b")
	if m.targetPick == nil || m.targetPick.top().list.cursor != 1 || len(m.targetList.all) != 0 {
		t.Fatalf("first click: picker %v cursor, targets %v, want row 1 marked and nothing taken", m.targetPick, m.targetList.all)
	}
	clickOn(t, m, "Group b")
	if m.targetPick != nil || len(m.targetList.all) != 1 || m.targetList.all[0] != "g/b" {
		t.Fatalf("click on the marked entry: picker %v targets %v, want g/b taken", m.targetPick, m.targetList.all)
	}
}

func TestClickOnARowOfTheTargetBuilderMarksItAndASecondClickChoosesIt(t *testing.T) {
	src := &listSource{list: pickPage(group("a"))}
	m := pickerModel(t, src)
	m.prefs.Icons = kitprefs.IconsNone
	m.render()
	if m.targetAdd == nil || m.targetAdd.kind >= 0 {
		t.Fatalf("add menu = %+v", m.targetAdd)
	}
	clickOn(t, m, "from the provider's list")
	if m.targetPick != nil || m.targetAdd == nil {
		t.Fatalf("first click chose the row")
	}
	clickOn(t, m, "from the provider's list")
	if m.targetPick == nil {
		t.Fatalf("click on the marked row did not open the picker:\n%s", screenOf(m))
	}
}

func TestClickOnAnExportRowMarksItAndItsBoxTicksIt(t *testing.T) {
	v, _ := plainExportModel(t)
	m := v.m
	m.prefs.Icons = kitprefs.IconsNone
	v.startExport("")
	m.render()
	clickOn(t, m, "docs")
	e := m.export
	if id, _ := e.list.Selected(); e.entries[atoi(t, id)] != exportConnPref+"docs" || e.ticked() != 0 {
		t.Fatalf("first click: marked %q ticked %d, want the row marked and nothing ticked", id, e.ticked())
	}
	clickOn(t, m, "[ ] docs")
	if e.ticked() != 1 || e.step != exportSelect {
		t.Fatalf("click on the box: ticked %d step %v, want it ticked and the selection kept", e.ticked(), e.step)
	}
	clickOn(t, m, "[x] docs")
	if e.ticked() != 0 {
		t.Fatalf("second click on the box ticked %d, want it unticked", e.ticked())
	}
	clickOn(t, m, "docs")
	if m.fail != tr(tExportTickFirst) || e.step != exportSelect {
		t.Fatalf("click on the marked row with nothing ticked: step %v fail %q, want enter's refusal", e.step, m.fail)
	}
}

func atoi(t *testing.T, id kittui.ItemID) int {
	t.Helper()
	i, err := strconv.Atoi(string(id))
	if err != nil {
		t.Fatalf("the ID %q is no index", id)
	}
	return i
}

func TestClickOnAnExportModeMarksItAndASecondClickChoosesIt(t *testing.T) {
	v, _ := plainExportModel(t)
	m := v.m
	m.prefs.Icons = kitprefs.IconsNone
	v.startExport("")
	v.pump("up", " ", "enter")
	if m.export.step != exportMode || m.export.secretsReason == "" {
		t.Fatalf("step %v reason %q, want the mode question with secrets unavailable", m.export.step, m.export.secretsReason)
	}
	m.render()
	// A row that cannot be chosen marks like any, and choosing it only says why.
	clickOn(t, m, "with secrets, encrypted")
	if i, _ := chosen(m.export.mode, 3); i != 1 {
		t.Fatalf("click on a blocked row marked %d, want 1", i)
	}
	clickOn(t, m, "with secrets, encrypted")
	if m.export.step != exportMode || m.fail != m.export.secretsReason {
		t.Fatalf("choosing a blocked row: step %v fail %q, want the reason and no step", m.export.step, m.fail)
	}
	clickOn(t, m, "without secrets")
	clickOn(t, m, "without secrets")
	if m.export.step != exportTarget {
		t.Fatalf("click on the marked mode: step %v, want the file question", m.export.step)
	}
}

func TestClickOnAnExportModeNeedsTheAdminSessionLikeEnter(t *testing.T) {
	v, _ := vaultExportModel(t)
	m := v.m
	m.prefs.Icons = kitprefs.IconsNone
	v.startExport(testExportVaultPass)
	v.pump("up", " ", "enter")
	if m.export.step != exportMode || m.export.secretsReason != "" {
		t.Fatalf("step %v reason %q, want the mode question", m.export.step, m.export.secretsReason)
	}
	m.render()
	row := "with secrets, encrypted"
	clickOn(t, m, row)
	if i, _ := chosen(m.export.mode, 3); i != 1 || m.export.step != exportMode {
		t.Fatalf("first click: marked %d step %v, want only the mark", i, m.export.step)
	}
	m.endAdminSession()
	clickOn(t, m, row)
	if m.screen != screenAdminAuth {
		t.Fatalf("click on the marked mode: screen %v, want the admin question enter asks", m.screen)
	}
}

func TestClickOnAnImportModeMarksItAndASecondClickPreviews(t *testing.T) {
	v := plainImportTarget(t)
	m := v.m
	m.prefs.Icons = kitprefs.IconsNone
	path := importFixture(t, exportWithout)
	v.startImport("")
	v.typePath(path)
	v.pump("enter")
	if m.imp.step != importMode {
		t.Fatalf("step %v fail %q, want the mode question", m.imp.step, m.fail)
	}
	m.render()
	text := strings.Fields(tr(importModeRows[1].text))[0]
	clickOn(t, m, text)
	if m.imp.modeCursor != 1 || m.imp.step != importMode {
		t.Fatalf("first click: cursor %d step %v, want only the mark", m.imp.modeCursor, m.imp.step)
	}
	clickOn(t, m, text)
	if m.imp.step == importMode {
		t.Fatalf("click on the marked mode did not go on:\n%s", screenOf(m))
	}
}

func TestClickOnAFindingMarksItAndASecondClickStartsIt(t *testing.T) {
	s := newFindScene(t)
	s.renameByHand(t, "credential", "reader", "reader2")
	m := s.open(t)
	m.prefs.Icons = kitprefs.IconsNone
	openSectionByName(t, m, sectionCredentials)
	m.openFindings()
	if m.screen != screenFindings || len(m.findings.items) != 1 {
		t.Fatalf("screen %v findings %+v, want the one hand rename", m.screen, m.findings)
	}
	// A second finding that has no action stands first, so the real one is not the marked row.
	items := append([]renameFinding{{kind: "connection", entry: "x", from: "x", to: "y", ambiguous: true}}, m.findings.items...)
	list, err := m.findingsListFor(items, 0)
	mustNoError(t, err)
	m.findings.items, m.findings.list = items, list
	m.render()
	clickOn(t, m, "reader2")
	if at, _ := chosen(m.findings.list, 2); m.screen != screenFindings || at != 1 {
		t.Fatalf("first click: screen %v marked %d, want finding 1 marked", m.screen, at)
	}
	clickOn(t, m, "reader2")
	if m.screen != screenRename || m.rename == nil {
		t.Fatalf("click on the marked finding: screen %v, want the dry run enter starts", m.screen)
	}
}

func TestClickOnAPathSuggestionMarksItAndASecondClickTakesIt(t *testing.T) {
	v, _ := plainExportModel(t)
	m := v.m
	m.prefs.Icons = kitprefs.IconsNone
	// Relative names keep the rows short: a suggestion that wraps over several lines is no click target.
	dir := t.TempDir()
	for _, name := range []string{"alpha", "abeta", "agamma"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	v.startExport("")
	v.pump("up", " ", "enter")
	v.pump("enter")
	if m.export.step != exportTarget {
		t.Fatalf("step %v, want the file question", m.export.step)
	}
	v.typePath("a")
	if view := askedText(m); !strings.Contains(view, "alpha") || !strings.Contains(view, "abeta") ||
		!strings.Contains(view, "agamma") {
		t.Fatalf("the three directories are not suggested:\n%s", view)
	}
	clickOn(t, m, "agamma")
	if typedInput(m) != "a" {
		t.Fatalf("first click: value %q, want only the mark", typedInput(m))
	}
	clickOn(t, m, "agamma")
	if want := "agamma" + string(filepath.Separator); typedInput(m) != want {
		t.Fatalf("click on the marked suggestion: value %q, want %q (tab completes)", typedInput(m), want)
	}
	if m.export.step != exportTarget {
		t.Fatalf("the click went on to step %v", m.export.step)
	}
}
