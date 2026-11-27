package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// viewHasNoControl fails the test where the view holds a control sequence of the provider's names.
func viewHasNoControl(t *testing.T, view string) {
	t.Helper()
	for _, bad := range []string{"\x07", "\x1b]", "\x1b[2J"} {
		if strings.Contains(view, bad) {
			t.Fatalf("the view holds the control sequence %q:\n%s", bad, view)
		}
	}
}

func TestTargetNamesAndLabelsWithControlCharactersAreEscapedAndNeverIDs(t *testing.T) {
	const evil = "repos/x\x1b]0;owned\x07\x1b[2Jy"
	m, _, _ := knownTargetsModel(t)
	mustNoError(t, m.cfg.SetConnection("gh-evil", config.Connection{Service: "zeta", Credential: "reader", Target: evil}))
	openConnection(t, m, "gh-a")
	openTargetList(t, m)
	m.targetList.all = append(m.targetList.all, evil)
	m.targetLabels = map[string]string{evil: "l\x1b[31m\x07abel"}
	m.showTargets(0)
	for _, id := range m.targetList.list.Shown() {
		if _, err := strconv.Atoi(string(id)); err != nil {
			t.Fatalf("the entry ID %q is no index", id)
		}
	}
	view := m.render()
	viewHasNoControl(t, view)
	if strings.ContainsRune(ansi.Strip(view), 0x1b) || !strings.Contains(ansi.Strip(view), "repos/x") {
		t.Fatalf("the target does not stand as filtered text:\n%s", ansi.Strip(view))
	}
	// The add menu offers what other connections use through the same filter.
	press(t, m, "n")
	for _, id := range m.targetAdd.choices.Shown() {
		if _, err := strconv.Atoi(string(id)); err != nil {
			t.Fatalf("the menu ID %q is no index", id)
		}
	}
	viewHasNoControl(t, m.render())
}

func TestClickOnATargetRowMarksItAndASecondClickEditsIt(t *testing.T) {
	m, _, _ := plainTargetsModel(t)
	m.prefs.Icons = kitprefs.IconsNone
	openConnection(t, m, "gh")
	openTargetList(t, m)
	m.render()
	clickOn(t, m, "repos/octo/b")
	if _, at, _ := m.targetSelected(); at != 1 || m.targetEdit >= 0 {
		t.Fatalf("first click: row %d, edit %d, want the second row marked and nothing typed", at, m.targetEdit)
	}
	m.render()
	clickOn(t, m, "repos/octo/b")
	if m.targetEdit != 1 || m.targetInput.Value() != "repos/octo/b" {
		t.Fatalf("click on the marked row: edit %d %q, want the target typed as enter does", m.targetEdit, m.targetInput.Value())
	}
}

func TestTheAddMenuCountsTheShownRowsAndFirstEscClearsTheLine(t *testing.T) {
	m, _, _ := knownTargetsModel(t)
	openConnection(t, m, "gh-a")
	openTargetList(t, m)
	press(t, m, "n")
	all := len(m.targetAdd.choices.Shown())
	typeText(t, m, "repository")
	shown := m.targetAdd.choices.Shown()
	if len(shown) == 0 || len(shown) >= all || !strings.Contains(m.targetAdd.choices.Position(), strconv.Itoa(len(shown))) {
		t.Fatalf("shown %d of %d, position %q: the count does not follow the shown rows", len(shown), all, m.targetAdd.choices.Position())
	}
	if first, _ := m.targetAdd.selected(); first != addTyped {
		t.Fatalf("the typed line is not marked first: %q", first)
	}
	press(t, m, "esc")
	if m.targetAdd == nil || m.targetAdd.query() != "" || len(m.targetAdd.choices.Shown()) != all || slices.Contains(m.targetList.all, "repository") {
		t.Fatalf("the first esc did not only clear the line: menu %v", m.targetAdd != nil)
	}
}

func TestTheTargetListAndTheAddMenuDrawAtEverySize(t *testing.T) {
	for _, size := range [][2]int{{40, 15}, {59, 19}, {80, 24}, {150, 42}} {
		m, _, _ := knownTargetsModel(t)
		m = sizedModel(m, size[0], size[1])
		openConnection(t, m, "gh-a")
		openTargetList(t, m)
		if view := m.render(); view == "" {
			t.Fatalf("%v: the target list drew nothing", size)
		}
		press(t, m, "n")
		if view := m.render(); m.targetAdd == nil || view == "" {
			t.Fatalf("%v: the add menu drew nothing", size)
		}
	}
}
