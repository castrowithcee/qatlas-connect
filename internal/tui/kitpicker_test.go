package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// accessPicker opens the picker of the access row of a new connection, which holds by effect.
func accessPicker(t *testing.T) *Model {
	t.Helper()
	m := connectionForm(t, 100)
	selectAccess(t, m, accessEffects)
	press(t, m, "enter")
	if !valuePickerOpen(m) {
		t.Fatalf("enter on the access row opened screen %v, want the picker", m.screen)
	}
	m.render()
	return m
}

// The picker starts on the value in force, which its own column names; moving changes nothing, and only enter
// puts the marked value on the row, with the rows that depend on it in line.
func TestTheValuePickerStartsOnTheValueInForceAndChoosesOnEnter(t *testing.T) {
	m := accessPicker(t)
	if choice, _ := m.valuePick.selected(); choice != accessEffects {
		t.Fatalf("the picker starts on %q, want the value in force %q", choice, accessEffects)
	}
	if view := ansi.Strip(m.render()); !regexp.MustCompile("> "+accessEffects+" +current").MatchString(view) ||
		!strings.Contains(view, "Choose access") {
		t.Fatalf("the picker does not name the row and the value in force:\n%s", view)
	}
	for range m.valuePick.choices {
		if choice, _ := m.valuePick.selected(); choice == accessTools {
			break
		}
		press(t, m, "down")
	}
	if got := m.fieldValue(accessLabel); got != accessEffects {
		t.Fatalf("moving in the picker changed the row to %q", got)
	}
	press(t, m, "enter")
	if m.modal != nil || m.valuePick != nil || m.fieldValue(accessLabel) != accessTools {
		t.Fatalf("enter left the picker open %v and the row at %q", m.modal != nil, m.fieldValue(accessLabel))
	}
	if m.field(toolListLabel).hidden || !m.field(effectsLabel).hidden {
		t.Error("choosing selected tools did not bring the tool list and the effects in line")
	}
}

// Cancel, esc and a click beside the frame leave the row and the whole form as they were, and a click beside
// the frame reaches nothing of the screen behind it.
func TestEveryWayOutOfThePickerLeavesTheRow(t *testing.T) {
	for _, way := range []string{"cancel", "esc", "beside"} {
		m := accessPicker(t)
		before, focus := m.formState(), m.focus
		press(t, m, "down")
		m.render()
		switch way {
		case "cancel":
			clickOn(t, m, "esc cancel")
		case "esc":
			press(t, m, "esc")
		case "beside":
			clickAt(t, m, 0, m.termHeight/2)
		}
		if m.modal != nil || m.valuePick != nil || m.screen != screenForm {
			t.Fatalf("%s left the picker open on screen %v", way, m.screen)
		}
		if m.formState() != before || m.focus != focus || m.section != sectionConnections {
			t.Errorf("%s changed the form: row %q focus %d section %v", way, m.fieldValue(accessLabel), m.focus, m.section)
		}
	}
}

// Ctrl+c with changed ticks asks the leave question about them, and keeping on editing returns to the picker
// with the ticks.
func TestCtrlCAsksForTheChangedTicksOfThePicker(t *testing.T) {
	m := connectionForm(t, 100)
	selectAccess(t, m, accessEffects)
	focusField(t, m, effectsLabel)
	press(t, m, "enter")
	typeText(t, m, "create")
	press(t, m, " ")
	_, cmd := m.Update(keyMsg("ctrl+c"))
	deliver(m, cmd)
	if view := screenOf(m); m.quitting || m.screen != screenLeave || !strings.Contains(view, "the ticks changed") {
		t.Fatalf("ctrl+c on changed ticks: quitting %v screen %v\n%s", m.quitting, m.screen, view)
	}
	if m.modal == nil || m.modal != kittui.Modal(m.question) || len(m.modalsUnder) != 0 || m.baseScreen() != screenForm {
		t.Fatalf("the question is not the one dialog over the form: base %v", m.baseScreen())
	}
	press(t, m, "esc")
	if !valuePickerOpen(m) || !m.pickerMarks["create"] {
		t.Fatalf("keeping on editing left screen %v with the ticks %v", m.screen, m.pickerMarks)
	}
}

// Space ticks while a search is typed, unless a value holds a space: that value must stay searchable, so
// space then types.
func TestSpaceTicksWhileTypingOnlyWhereNoValueHoldsASpace(t *testing.T) {
	for _, spaced := range []bool{false, true} {
		m := connectionForm(t, 100)
		selectAccess(t, m, accessEffects)
		focusField(t, m, effectsLabel)
		if spaced {
			m.fields[m.focus].choices = append(m.fields[m.focus].choices, "two words")
		}
		press(t, m, "enter")
		typeText(t, m, "crea")
		press(t, m, "space")
		query, ticked := m.valuePick.list.Query(), m.pickerMarks["create"]
		if spaced && (query != "crea " || ticked) || !spaced && (query != "crea" || !ticked) {
			t.Errorf("a value with a space %v: space left the search %q and the tick %v", spaced, query, ticked)
		}
	}
}

// A value is drawn through the log filter: a control character never reaches the terminal.
func TestAValueWithAControlCharacterIsDrawnHarmless(t *testing.T) {
	m := connectionForm(t, 100)
	selectAccess(t, m, accessEffects)
	focusField(t, m, effectsLabel)
	m.fields[m.focus].choices = append(m.fields[m.focus].choices, "evil\x1b]52;c;eA==\x07\x1b[2Jname")
	press(t, m, "enter")
	if !valuePickerOpen(m) {
		t.Fatalf("the picker did not open: %q", m.fail)
	}
	view := m.render()
	if strings.Contains(view, "\x1b]52") || strings.Contains(view, "\x07") || strings.Contains(view, "\x1b[2J") {
		t.Fatalf("the control characters of a value reach the terminal:\n%q", view)
	}
	if !strings.Contains(ansi.Strip(view), "evil�]52;c;eA==��[2Jname") {
		t.Errorf("the value is not drawn with its control characters replaced:\n%s", ansi.Strip(view))
	}
}

// The picker stands over the form where it fits and fills the workspace where it does not, at every size the
// editor supports, with the marked value and its keys on screen.
func TestTheValuePickerFitsEverySize(t *testing.T) {
	for _, size := range [][2]int{{minimumWidth, minimumHeight}, {59, 19}, {80, 24}, {150, 42}} {
		m, _, _ := newModel(t)
		seedConnections(t, m, numberedConnections(60)...)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		openDefaultForm(t, m)
		press(t, m, "/")
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := screenOf(m)
		assertViewFits(t, view, size[0], size[1])
		if !valuePickerOpen(m) || !strings.Contains(view, "> conn-000") || !strings.Contains(view, "esc cancel") ||
			strings.Contains(view, "Resize terminal") {
			t.Errorf("the picker at %dx%d:\n%s", size[0], size[1], view)
		}
	}
}

// The info key explains the marked value over the picker, which comes back when the explanation closes; the
// letter stays text of the search, so only the extra info key does so there.
func TestTheInfoKeyExplainsTheMarkedValueOverThePicker(t *testing.T) {
	keepKeys(t)
	applyKeys(map[action]string{actInfo: "f3"})
	m := accessPicker(t)
	press(t, m, "i")
	if infoOpen(m) || m.valuePick.list.Query() != "i" {
		t.Fatalf("the info letter is no search text: explanation %v search %q", infoOpen(m), m.valuePick.list.Query())
	}
	press(t, m, "backspace", "f3")
	if !infoOpen(m) {
		t.Fatalf("the extra info key opened nothing:\n%s", m.render())
	}
	press(t, m, "esc")
	if infoOpen(m) || !valuePickerOpen(m) {
		t.Fatalf("closing the explanation left explanation %v and screen %v, want the picker", infoOpen(m), m.screen)
	}
}
