package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// drag presses the left button at x0, y0, moves with it held to x1, y1 and releases it there, the way a
// terminal reports a drag, and returns what the release hands to the clipboard.
func drag(t *testing.T, m *Model, x0, y0, x1, y1 int) (string, bool) {
	t.Helper()
	_, cmd := m.Update(tea.MouseClickMsg{X: x0, Y: y0, Button: tea.MouseLeft})
	deliver(m, cmd)
	_, cmd = m.Update(tea.MouseMotionMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	deliver(m, cmd)
	_, cmd = m.Update(tea.MouseReleaseMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	return clipboardOf(cmd)
}

// dragOver drags from the first cell of from to the last cell of to, both as the frame draws them.
func dragOver(t *testing.T, m *Model, from, to string) (string, bool) {
	t.Helper()
	x0, y0 := cellOf(t, m, from)
	x1, y1 := cellOf(t, m, to)
	return drag(t, m, x0, y0, x1+ansi.StringWidth(to)-1, y1)
}

// helpOverConnections is the editor on Connections with the help open on its first topic.
func helpOverConnections(t *testing.T) *Model {
	t.Helper()
	m := sizedModel(groupModel(t), 120, 40)
	openSectionByName(t, m, sectionConnections)
	press(t, m, "?")
	if !helpOpen(m) {
		t.Fatal("? opened no help")
	}
	return m
}

// A drag over the help and over an explanation hands exactly the drawn text to the clipboard, a sentence over
// several rows as one line, without frame, tabs, keys or position line, says so as the copy key does, and
// leaves the screen as it was.
func TestADragOverTheHelpAndAnExplanationCopiesTheDrawnText(t *testing.T) {
	cases := map[string]struct {
		open     func(t *testing.T) *Model
		from, to string
		want     string
	}{
		"help, one row": {open: helpOverConnections, from: "Qatlas Connect reaches", to: "through named",
			want: "Qatlas Connect reaches the services you already run through named"},
		"help, a sentence over two rows": {open: helpOverConnections, from: "Qatlas Connect reaches", to: "a few minutes:",
			want: "Qatlas Connect reaches the services you already run through named connections. A first session takes a few minutes:"},
		"explanation": {open: func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 120, 40)
			openSectionByName(t, m, sectionConnections)
			press(t, m, "i")
			if !infoOpen(m) {
				t.Fatal("i opened no explanation")
			}
			return m
		}, from: "A connection is", to: "description.",
			want: "A connection is the route an agent takes: one service with one credential, an optional target, and the effects and tools it may use. Agents see only its name and description."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := c.open(t)
			modal, screen, section := m.modal, m.screen, m.section
			got, ok := dragOver(t, m, c.from, c.to)
			if !ok || got != c.want {
				t.Fatalf("the drag copied %q (%v), want %q", got, ok, c.want)
			}
			if !strings.Contains(m.status, "terminal") {
				t.Errorf("status = %q, want the hand-over to the terminal", m.status)
			}
			if m.modal != modal || m.screen != screen || m.section != section {
				t.Errorf("the drag changed the screen: modal %v screen %v section %v", m.modal != modal, m.screen, m.section)
			}
		})
	}
}

// A drag over the rows of a list copies them and marks, opens and focuses nothing; a press on such a row acts
// only at its release without motion, as the click it was.
func TestADragActsOnNothingAndAClickActsAtItsRelease(t *testing.T) {
	m := sizedModel(groupModel(t), 120, 40)
	openSectionByName(t, m, sectionConnections)
	selectRow(m, "wiki-docs")
	focus := func() [3]bool { return [3]bool{m.barFocused(), m.detailFocused(), m.onOptionBar()} }
	cursor, screen, zone := listCursor(m), m.screen, focus()
	got, ok := dragOver(t, m, "tracker-issues", "tracker-triage")
	if !ok || !strings.Contains(got, "tracker-issues") || !strings.Contains(got, "tracker-triage") {
		t.Fatalf("the drag copied %q (%v), want both rows", got, ok)
	}
	if listCursor(m) != cursor || m.screen != screen || focus() != zone {
		t.Errorf("the drag acted: cursor %d screen %v focus %v, want %d %v %v", listCursor(m), m.screen, focus(), cursor, screen, zone)
	}

	x, y := cellOf(t, m, "tracker-issues")
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	deliver(m, cmd)
	if listCursor(m) != cursor || m.selection.Active() == false {
		t.Fatalf("the press acted at once: cursor %d, want %d held", listCursor(m), cursor)
	}
	_, cmd = m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if text, ok := clipboardOf(cmd); ok {
		t.Fatalf("a click copied %q", text)
	}
	if name, _ := m.marked(); name != "tracker-issues" || m.screen != screen {
		t.Errorf("the release marked %q on screen %v, want tracker-issues marked as a click does", name, m.screen)
	}
}

// Every key ends the selection and its highlight.
func TestEveryKeyEndsTheSelection(t *testing.T) {
	for _, key := range []string{"down", "x", "tab"} {
		m := helpOverConnections(t)
		if _, ok := dragOver(t, m, "Qatlas Connect reaches", "through named"); !ok || !m.selection.Active() {
			t.Fatalf("the drag selected nothing")
		}
		press(t, m, key)
		if m.selection.Active() {
			t.Errorf("%s left the selection", key)
		}
	}
}

// Without the mouse, in a terminal too small for the screen, while a key is captured and beside a question
// that takes no click outside its frame, a drag selects and copies nothing and changes nothing.
func TestADragDoesNothingWhereTheMouseDoesNot(t *testing.T) {
	cases := map[string]func(m *Model){
		"mouse off":    func(m *Model) { m.prefs.MouseEnabled = false },
		"too small":    func(m *Model) { m.Update(tea.WindowSizeMsg{Width: minimumWidth - 1, Height: minimumHeight - 1}) },
		"key captured": func(m *Model) { m.keyCapture = true },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			m := helpOverConnections(t)
			x0, y0 := cellOf(t, m, "Qatlas Connect reaches")
			set(m)
			modal, screen := m.modal, m.screen
			if text, ok := drag(t, m, x0, y0, x0+30, y0+1); ok || m.selection.Active() {
				t.Errorf("the drag copied %q, selection %v", text, m.selection.Active())
			}
			if m.modal != modal || m.screen != screen {
				t.Errorf("the drag changed the screen to %v", m.screen)
			}
		})
	}
	t.Run("beside a question", func(t *testing.T) {
		m := sizedModel(groupModel(t), 120, 40)
		openSectionByName(t, m, sectionConnections)
		selectRow(m, "tracker-triage")
		x0, y0 := cellOf(t, m, "wiki-docs")
		press(t, m, "d")
		if m.question == nil {
			t.Fatal("d asked nothing")
		}
		question := m.question
		if text, ok := drag(t, m, x0, y0, x0+12, y0); ok || m.selection.Active() {
			t.Errorf("the drag beside the question copied %q, selection %v", text, m.selection.Active())
		}
		if m.question != question {
			t.Error("the drag beside the question answered it")
		}
	})
}

// A control character of a drawn name reaches the clipboard only masked.
func TestADragCopiesControlCharactersMasked(t *testing.T) {
	m := sizedModel(groupModel(t), 120, 40)
	openSectionByName(t, m, sectionServices)
	pressNew(t, m)
	focusField(t, m, providerLabel)
	m.fields[m.focus].choices = append(m.fields[m.focus].choices, evil)
	press(t, m, "enter")
	if m.modal == nil {
		t.Fatalf("the provider table did not open: screen %v fail %q", m.screen, m.fail)
	}
	x, y := cellOf(t, m, "evil")
	got, ok := drag(t, m, x, y, x+30, y)
	if !ok || !strings.HasPrefix(got, "evil") {
		t.Fatalf("the drag copied %q (%v), want the evil name", got, ok)
	}
	noControl(t, "the copied name", got)
}

// A value the editor masks or draws itself never enters the selection: neither a typed passphrase nor a
// revealed token value reaches the clipboard, whatever is dragged over.
func TestADragNeverCopiesAMaskedValue(t *testing.T) {
	t.Run("passphrase", func(t *testing.T) {
		const typed = "canary-typed-phrase"
		m := passphrasePrompt(t)
		typeText(t, m, typed)
		mask := masked(len(typed))
		text, ok := dragOver(t, m, "this also starts", mask)
		if !ok || !strings.Contains(text, "this also starts") || strings.Contains(text, typed) {
			t.Fatalf("a drag over the dialog copied %q (%v), want its text without the passphrase", text, ok)
		}
		x, y := cellOf(t, m, mask)
		if text, ok := drag(t, m, x, y, x+len(typed), y); ok && strings.Contains(text, typed) {
			t.Fatalf("a drag over the masked line copied the passphrase: %q", text)
		}
		if typedInput(m) != typed {
			t.Errorf("the drag changed the input to %q", typedInput(m))
		}
	})
	t.Run("token value", func(t *testing.T) {
		store, dir, v := newTokenFixture(t)
		token := fixtureToken(t, v, "kunde-a-ci", nil, "personal")
		m := openApprovalModel(t, store, dir)
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		openTokensUnlocked(t, m)
		pump(t, m, "enter", "V")
		x, y := cellOf(t, m, token.Value)
		if text, ok := drag(t, m, x, y, x+ansi.StringWidth(token.Value)-1, y); ok && strings.Contains(text, token.Value) {
			t.Fatalf("the drag copied the token value: %q", text)
		}
		if m.selection.Active() {
			t.Error("the drawn token value is a selection target")
		}
	})
}
