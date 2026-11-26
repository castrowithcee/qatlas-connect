package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// atSettingsBar reports whether the click on the Settings tab went through: the tab line has the focus on it.
func atSettingsBar(m *Model) bool {
	return m.section.area() == areaSettings && m.screen == screenNav && m.wizard == nil
}

// A click on a tab ends a typed line the way esc does, and then goes on.
func TestTabClickEndsATypedLine(t *testing.T) {
	m := targetsScreen(t)
	press(t, m, "n")
	typeText(t, m, "repos/octo/x")
	clickOn(t, m, "6 Settings")
	if m.typing() || m.screen == screenTargets {
		t.Errorf("a click while a target is typed: screen %v typing %v", m.screen, m.typing())
	}
	if m.screen != screenLeave && !atSettingsBar(m) {
		t.Errorf("the click neither asked nor reached Settings: screen %v section %v", m.screen, m.section)
	}

	m = sizedModel(groupModel(t), 160, 40)
	openSectionByName(t, m, sectionServices)
	press(t, m, "/")
	typeText(t, m, "wiki")
	if !m.typing() {
		t.Fatal("the filter is not typed")
	}
	clickOn(t, m, "6 Settings")
	if !atSettingsBar(m) {
		t.Errorf("a click while a filter is typed: screen %v section %v", m.screen, m.section)
	}
}

// A typed text row of a form keeps the text behind the standard question.
func TestTabClickOverATypedFormAsks(t *testing.T) {
	m := sizedModel(groupModel(t), 160, 40)
	openSectionByName(t, m, sectionServices)
	pressNew(t, m)
	typeText(t, m, "draft")
	clickOn(t, m, "6 Settings")
	if m.screen != screenLeave || m.section != sectionServices {
		t.Fatalf("a click over typed text: screen %v section %v, want the leave question", m.screen, m.section)
	}
	clickOn(t, m, "6 Settings")
	if m.screen != screenLeave {
		t.Errorf("a click over the question: screen %v, want it still open", m.screen)
	}
	press(t, m, "d")
	if m.screen == screenLeave || m.fieldValue("name") == "draft" {
		t.Errorf("d did not discard: screen %v", m.screen)
	}
}

// An open question, a passphrase prompt and a key capture are answered by their keys alone.
func TestTabClickDoesNothingWhileAsked(t *testing.T) {
	m := sizedModel(groupModel(t), 160, 40)
	openSectionByName(t, m, sectionServices)
	press(t, m, "d")
	if !m.questionScreen() {
		t.Fatalf("d did not ask: screen %v", m.screen)
	}
	asked := m.screen
	clickOn(t, m, "6 Settings")
	if m.screen != asked || m.section != sectionServices {
		t.Errorf("a click over a confirmation: screen %v section %v", m.screen, m.section)
	}

	m, _ = newEncryptedVaultModel(t, filepath.Join(t.TempDir(), "qconnect"), "hunter2", true)
	sizedModel(m, 160, 40)
	openSectionByName(t, m, sectionServices)
	pressNew(t, m)
	typeText(t, m, "wiki-primary")
	press(t, m, "tab", "tab")
	typeText(t, m, "https://wiki.example.invalid")
	press(t, m, "f2")
	if m.screen != screenAdminAuth {
		t.Fatalf("saving did not ask for the passphrase: screen %v", m.screen)
	}
	clickOn(t, m, "6 Settings")
	if m.screen != screenAdminAuth || m.section != sectionServices {
		t.Errorf("a click over a passphrase prompt: screen %v section %v", m.screen, m.section)
	}

	m, _ = lockedVaultModel(t)
	m.prefs.Icons = uiprefs.IconsNone
	sizedModel(m, 160, 40)
	openSectionByName(t, m, sectionGeneral)
	focusKeyRow(t, m, "info key")
	press(t, m, "enter")
	if !m.keyCapture {
		t.Fatal("enter did not start the capture")
	}
	clickOn(t, m, "6 Settings")
	if !m.keyCapture || m.screen != screenForm {
		t.Errorf("a click while a key is captured: capture %v screen %v", m.keyCapture, m.screen)
	}
}

// Overlays without a question that are no modal of the kit close the way esc closes them, and the click goes
// on.
func TestTabClickClosesAnOverlayAndActs(t *testing.T) {
	cases := []struct {
		name   string
		open   func(t *testing.T) *Model
		isOpen func(m *Model) bool
	}{
		{"command line", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 160, 40)
			openSectionByName(t, m, sectionServices)
			press(t, m, ":")
			return m
		}, func(m *Model) bool { return m.screen == screenCommand }},
		{"explanation", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 160, 40)
			openSectionByName(t, m, sectionServices)
			pressNew(t, m)
			press(t, m, "tab")
			m.infoOpen = true
			return m
		}, func(m *Model) bool { return m.infoOpen }},
		{"dropdown", func(t *testing.T) *Model {
			m := newTableModel(t, 100)
			openSectionByName(t, m, sectionConnections)
			press(t, m, "tab", "right", "left", "enter")
			return m
		}, func(m *Model) bool { return m.dd != nil }},
	}
	for _, c := range cases {
		m := c.open(t)
		if !c.isOpen(m) {
			t.Fatalf("%s: not open:\n%s", c.name, screenOf(m))
		}
		clickOn(t, m, "6 Settings")
		if c.isOpen(m) || !atSettingsBar(m) {
			t.Errorf("%s: after the click: screen %v section %v", c.name, m.screen, m.section)
		}
	}
}

// helpBox is the cells the frame of the help drawn over the screen covers.
func helpBox(t *testing.T, m *Model) kittui.Rect {
	t.Helper()
	lines := frameLines(m)
	for y, line := range lines {
		if !strings.Contains(line, "═ Help ") {
			continue
		}
		left := ansi.StringWidth(line[:strings.Index(line, "╔")])
		right := ansi.StringWidth(line[:strings.Index(line, "╗")])
		for b := y + 1; b < len(lines); b++ {
			if strings.Contains(lines[b], "╚") {
				return kittui.Rect{X: left, Y: y, Width: right - left + 1, Height: b - y + 1}
			}
		}
	}
	t.Fatalf("no help frame:\n%s", strings.Join(lines, "\n"))
	return kittui.Rect{}
}

// Under the help only its own targets act: a click on a tab, the header or the footer behind it switches
// nothing and runs no key. A click outside the frame of the help closes it, and one inside the frame beside
// its targets does nothing. In the workspace the help leaves the header and the tabs without a click.
func TestClicksBesideTheHelpCloseItAndRunNothing(t *testing.T) {
	open := func(t *testing.T, w, h int) *Model {
		m := sizedModel(groupModel(t), w, h)
		m.prefs.Icons = uiprefs.IconsNone
		openSectionByName(t, m, sectionServices)
		m.render()
		press(t, m, "?")
		if !helpOpen(m) {
			t.Fatal("? did not open the help")
		}
		return m
	}
	unchanged := func(t *testing.T, m *Model, what string, wantOpen bool) {
		t.Helper()
		if m.section != sectionServices || m.screen != screenList || m.cmdline != nil || helpOpen(m) != wantOpen {
			t.Errorf("%s: section %v screen %v command line %v help open %v, want the list with the help open %v",
				what, m.section, m.screen, m.cmdline != nil, helpOpen(m), wantOpen)
		}
	}
	tabs := open(t, 160, 40)
	tabs.closeModal()
	tabs.render()
	inside, outside := 0, 0
	for _, tab := range tabs.frameTabs {
		m := open(t, 160, 40)
		box := helpBox(t, m)
		x, y := tab.Rect.X+(tab.Rect.Width-1)/2, tab.Rect.Y
		in := x >= box.X && x < box.X+box.Width && y >= box.Y && y < box.Y+box.Height
		if in {
			inside++
		} else {
			outside++
		}
		clickAt(t, m, x, y)
		unchanged(t, m, fmt.Sprintf("a click on the tab %s/%s %s", tab.Area, tab.Tab, map[bool]string{true: "under the frame", false: "beside the frame"}[in]), in)
	}
	if inside == 0 || outside == 0 {
		t.Errorf("%d tabs under and %d beside the frame of the help: the test reaches no case", inside, outside)
	}
	for _, text := range []string{"[?] help", "[:] cmd"} {
		m := open(t, 160, 40)
		clickOn(t, m, text)
		unchanged(t, m, "a click on the header key "+text, false)
	}
	m := open(t, 160, 40)
	lines := frameLines(m)
	clickAt(t, m, ansi.StringWidth(lines[len(lines)-1][:strings.Index(lines[len(lines)-1], "? help")]), len(lines)-1)
	unchanged(t, m, "a click on the footer", false)

	m = open(t, 160, 40)
	box := helpBox(t, m)
	clickAt(t, m, box.X+box.Width-2, box.Y+box.Height-3)
	unchanged(t, m, "a click inside the frame beside its targets", true)
	clickAt(t, m, box.X+box.Width+2, box.Y+box.Height/2)
	unchanged(t, m, "a click beside the frame", false)

	m = open(t, 43, 18)
	if strings.Contains(strings.Join(frameLines(m), "\n"), "═ Help ") {
		t.Fatal("the help stands over the screen at 43x18")
	}
	clickAt(t, m, 2, 0)
	unchanged(t, m, "a click on the header over the help in the workspace", true)
	clickAt(t, m, 3, 1)
	unchanged(t, m, "a click on the tab line over the help in the workspace", true)
}

// A dialog covers the tab line it stands over; a click there is a click on the dialog.
func TestTabClickUnderADialogIsNotATabClick(t *testing.T) {
	m := sizedModel(groupModel(t), 100, 24)
	openSectionByName(t, m, sectionServices)
	press(t, m, ":")
	m.render()
	box := m.dialogBox
	covered := 0
	for _, tab := range m.frameTabs {
		r := tab.Rect
		if box.x0 < r.X+r.Width && r.X < box.x1 && box.y0 <= r.Y && r.Y < box.y1 {
			covered++
			x, y := max(r.X, box.x0), r.Y
			if _, ok := m.clickTab(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}); ok {
				t.Fatalf("a tab under the dialog is a target at %d,%d", x, y)
			}
		}
	}
	if covered == 0 {
		t.Fatalf("the dialog %+v covers no tab", box)
	}
}

// In the guided setup a click on a tab goes through the leave question of the setup.
func TestTabClickInTheGuidedSetup(t *testing.T) {
	m, _, _, _, _ := newStoreModel(t)
	sizedModel(m, 160, 40)
	m.screen = screenNav
	press(t, m, "g")
	if m.wizard == nil {
		t.Fatalf("the setup did not start: %q", m.fail)
	}
	// Nothing chosen yet: the setup is left at once.
	clickOn(t, m, "6 Settings")
	if m.wizard != nil || !atSettingsBar(m) {
		t.Errorf("a click in an untouched setup: wizard %v screen %v", m.wizard != nil, m.screen)
	}

	m, _, _, _, _ = newStoreModel(t)
	sizedModel(m, 160, 40)
	walkSetup(t, m, stepService)
	clickOn(t, m, "6 Settings")
	if m.screen != screenLeave || m.wizard == nil {
		t.Fatalf("a click in a started setup: screen %v, want the leave question", m.screen)
	}
	clickOn(t, m, "6 Settings")
	if m.screen != screenLeave {
		t.Errorf("a click over the leave question: screen %v", m.screen)
	}
	press(t, m, "d")
	if m.wizard != nil {
		t.Errorf("d did not drop the setup")
	}

	// While the save is in flight nothing happens.
	m, _, _, _, _ = newStoreModel(t)
	sizedModel(m, 160, 40)
	walkSetup(t, m, stepSummary)
	m.wizard.saving = true
	clickOn(t, m, "6 Settings")
	if m.wizard == nil || m.screen != screenSummary {
		t.Errorf("a click while saving: screen %v", m.screen)
	}
	m.wizard.saving = false

	// A saved setup is finished first, and the click goes on.
	pump(t, m, "f2")
	if m.wizard == nil || m.wizard.saved == "" {
		t.Fatalf("the setup was not saved: %q", m.fail)
	}
	clickOn(t, m, "6 Settings")
	if m.wizard != nil || !atSettingsBar(m) {
		t.Errorf("a click after saving: wizard %v screen %v section %v", m.wizard != nil, m.screen, m.section)
	}
}
