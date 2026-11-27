package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// longText is n numbered words, so a view shows which of them it reaches.
func longText(n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = fmt.Sprintf("word%03d", i)
	}
	return strings.Join(words, " ")
}

func hasPosition(view string) bool {
	return strings.Contains(view, "lines ") && strings.Contains(view, " of ")
}

// The detail screen of a log entry scrolls a result longer than the screen instead of cutting it.
func TestLogDetailScrollsALongResult(t *testing.T) {
	f := newLogFixture(t)
	f.append("cli", "", "op.short", "wiki", "short result")
	f.append("cli", "", "op.long", "wiki", longText(400))
	m := f.model(true, f.localToday())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	open := func(op string) {
		m.screen = screenLogs
		m.logs.setFocus(logFocusRows, false)
		for i, row := range m.logs.rows {
			if row.line.Entry.Operation == op {
				m.logs.detail = i
			}
		}
		m.logs.text = nil
		m.screen = screenLogDetail
	}
	open("op.short")
	if view := screenOf(m); !strings.Contains(view, "response") {
		t.Fatalf("a short entry does not show every field:\n%s", view)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	open("op.long")
	view := screenOf(m)
	assertViewFits(t, view, 80, 18)
	if !hasPosition(view) || strings.Contains(view, "response") {
		t.Fatalf("a long entry does not offer to scroll:\n%s", view)
	}
	press(t, m, "end")
	view = screenOf(m)
	assertViewFits(t, view, 80, 18)
	if !strings.Contains(view, "response") {
		t.Fatalf("end does not reach the last line:\n%s", view)
	}
	// The whole value is there, wrapped over its lines: the copy and the view hold the same characters.
	press(t, m, "home")
	if !strings.Contains(screenOf(m), longText(400)[:60]) {
		t.Errorf("the start of the long result is not shown:\n%s", screenOf(m))
	}
	before := screenOf(m)
	wheelAt(t, m, 20, 10, tea.MouseWheelDown)
	if screenOf(m) == before {
		t.Errorf("a notch of the wheel did not scroll the detail")
	}
}

// The detail of an entry searches its text, wraps and numbers its lines, jumps between matches and goes back
// to the rows with esc, while the search is typed every key is text, and the copy is the text as it is drawn.
func TestLogDetailSearchWrapAndEsc(t *testing.T) {
	f := newLogFixture(t)
	f.append("cli", "", "wiki.pages.read", "wiki", longText(200))
	m := f.model(true, f.localToday())
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	press(t, m, "down", "enter")
	if m.screen != screenLogDetail {
		t.Fatalf("enter = screen %v, want the detail", m.screen)
	}
	// A typed search takes the letters of the keys and the digits of the areas as text.
	press(t, m, "/", "w", "c", "2")
	if m.screen != screenLogDetail || !m.logs.text.Searching() || m.logs.text.Query() != "wc2" {
		t.Fatalf("the typed search is %q on screen %v", m.logs.text.Query(), m.screen)
	}
	press(t, m, "esc")
	if m.screen != screenLogDetail || m.logs.text.Query() != "" || m.logs.text.Searching() {
		t.Fatalf("esc did not clear the search first")
	}
	press(t, m, "/")
	typeText(t, m, "wiki")
	press(t, m, "enter")
	view := screenOf(m)
	if m.logs.text.Query() != "wiki" || !strings.Contains(view, "wiki") {
		t.Fatalf("the search is not kept:\n%s", view)
	}
	press(t, m, "]", "[")
	if m.screen != screenLogDetail {
		t.Fatalf("a jump left the detail: screen %v", m.screen)
	}
	wrapped, numbers := m.logs.text.Wrapping(), m.logs.text.LineNumbers()
	press(t, m, "w", "#")
	if m.logs.text.Wrapping() == wrapped || m.logs.text.LineNumbers() == numbers {
		t.Errorf("w and # did not switch wrapping (%v) and numbers (%v)", m.logs.text.Wrapping(), m.logs.text.LineNumbers())
	}
	got, ok := pressCopy(m)
	if !ok || got != m.logDetailText(m.logs.rows[m.logs.detail]) {
		t.Errorf("c copied %q, want the text of the detail", got)
	}
	press(t, m, "esc", "esc")
	if m.screen != screenLogs {
		t.Fatalf("esc = screen %v, want the Logs screen", m.screen)
	}
}

// A control character and a terminal sequence in the log reach neither the detail nor the copy, and a secret
// of the run reaches neither.
func TestLogDetailShowsNoControlCharacterOrSecret(t *testing.T) {
	f := newLogFixture(t)
	f.append("cli", "cli\x1b]0;title\x07", "op.one", "wiki", "boom canary-detail-secret \x1b[31mred\x00\u202e end")
	m := f.model(true, f.localToday())
	m.redactor.Add("canary-detail-secret")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	press(t, m, "down", "enter")
	view := m.render()
	copied, ok := pressCopy(m)
	if !ok {
		t.Fatal("c handed nothing over")
	}
	for name, text := range map[string]string{"view": view, "copy": copied} {
		for _, bad := range []string{"\x1b[31m", "\x1b]0;", "\x07", "\x00", "\u202e", "canary-detail-secret"} {
			if strings.Contains(text, bad) {
				t.Errorf("the %s holds %q:\n%s", name, bad, text)
			}
		}
		if !strings.Contains(text, "boom") || !strings.Contains(text, "[redacted]") {
			t.Errorf("the %s lost the message or its redaction:\n%s", name, text)
		}
	}
	noControl(t, "detail copy", copied)
}

// longRawModel is a table model whose connection has many targets, so its raw tab outgrows the pane.
func longRawModel(t *testing.T) *Model {
	t.Helper()
	m := newTableModel(t, 140)
	conn := m.cfg.Connections["tracker-issues"]
	conn.Targets = nil
	conn.TargetLabels = map[string]string{}
	for i := range 60 {
		conn.Targets = append(conn.Targets, fmt.Sprintf("octo/repo%02d", i))
	}
	conn.Description = longText(300)
	m.cfg.Connections["tracker-issues"] = conn
	openSectionByName(t, m, sectionConnections)
	selectRow(m, "tracker-issues")
	return m
}

// The raw tab and the details tab of the detail pane reach their last line with the page keys, show the
// position only where they do not fit, and scroll under the wheel.
func TestDetailPaneScrollsLongTabs(t *testing.T) {
	short := newTableModel(t, 140)
	openSectionByName(t, short, sectionServices)
	selectRow(short, "wiki-main")
	if view := showRaw(t, short); hasPosition(view) {
		t.Fatalf("a short raw tab shows a position:\n%s", view)
	}

	m := longRawModel(t)
	view := showRaw(t, m)
	if !hasPosition(view) || strings.Contains(view, "octo/repo59") {
		t.Fatalf("a long raw tab does not offer to scroll:\n%s", view)
	}
	press(t, m, "end")
	if view = screenOf(m); !strings.Contains(view, "octo/repo59") {
		t.Fatalf("end does not reach the last line of the raw tab:\n%s", view)
	}
	press(t, m, "home")
	if m.detailPane.view.offset() != 0 {
		t.Fatalf("home left offset %d", m.detailPane.view.offset())
	}
	before := listCursor(m)
	press(t, m, "pgdown")
	if m.detailPane.view.offset() == 0 || listCursor(m) != before {
		t.Errorf("pgdown: pane offset %d, row %d -> %d", m.detailPane.view.offset(), before, listCursor(m))
	}
	press(t, m, "home")
	// The wheel over the pane scrolls it and leaves the rows alone.
	x, y := m.detailPane.x0+5, m.detailPane.y0+5
	wheelAt(t, m, x, y, tea.MouseWheelDown)
	if m.detailPane.view.offset() != wheelText || listCursor(m) != before {
		t.Errorf("the wheel moved the pane to %d and the row from %d to %d", m.detailPane.view.offset(), before, listCursor(m))
	}
	// Another tab starts at its first line.
	press(t, m, "left", "left")
	if view = screenOf(m); m.detailPane.view.offset() != 0 {
		t.Errorf("the overview tab starts at offset %d", m.detailPane.view.offset())
	}
	press(t, m, "end")
	if view = screenOf(m); !strings.Contains(view, "word031") {
		t.Errorf("end does not reach the last line of the overview:\n%s", view)
	}
}

// The raw tab keeps its secrets masked while it scrolls.
func TestDetailRawScrollKeepsSecretsMasked(t *testing.T) {
	m := rawModel(t)
	conn := m.cfg.Connections["tracker-issues"]
	for i := range 60 {
		conn.Targets = append(conn.Targets, fmt.Sprintf("octo/more%02d", i))
	}
	m.cfg.Connections["tracker-issues"] = conn
	openSectionByName(t, m, sectionServices)
	selectRow(m, "wiki-main")
	for _, name := range []string{"wiki-main", "token-host"} {
		selectRow(m, name)
		showRaw(t, m)
		for _, key := range []string{"end", "home", "pgdown"} {
			press(t, m, key)
			view := screenOf(m)
			for _, c := range []string{canaryURLPass, canaryQuery, canaryOption, canaryUser} {
				if strings.Contains(view, c) {
					t.Errorf("%s after %s shows %q", name, key, c)
				}
			}
		}
		m.leaveDetail()
	}
}

// The info overlay scrolls an explanation longer than itself, with the position and the keys named only then.
func TestInfoOverlayScrolls(t *testing.T) {
	m := connectionForm(t, 100)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m.openExplanation(infoSubject{title: "Long", long: "short text"})
	if view := m.render(); hasPosition(view) || strings.Contains(view, "scroll") {
		t.Fatalf("a short explanation shows a position:\n%s", view)
	}
	m.closeModal()
	m.openExplanation(infoSubject{title: "Long", long: longText(300)})
	view := m.render()
	if !hasPosition(view) || !strings.Contains(view, "↑/↓ scroll") || strings.Contains(view, "word299") {
		t.Fatalf("a long explanation does not offer to scroll:\n%s", view)
	}
	press(t, m, "end")
	if view = m.render(); !strings.Contains(view, "word299") {
		t.Fatalf("end does not reach the last line:\n%s", view)
	}
	press(t, m, "home")
	top := m.render()
	wheelAt(t, m, 20, 10, tea.MouseWheelDown)
	wheeled := m.render()
	press(t, m, "home", "down", "down", "down")
	if pressed := m.render(); !infoOpen(m) || wheeled == top || wheeled != pressed {
		t.Errorf("the wheel does not scroll as three presses of down: open %v\nwheel:\n%s\nkeys:\n%s", infoOpen(m), wheeled, pressed)
	}
	press(t, m, "esc")
	if infoOpen(m) {
		t.Error("esc did not close the overlay")
	}
}

// The explanation pane beside a form scrolls under the wheel when its text is longer than the pane.
func TestInfoPaneScrollsUnderTheWheel(t *testing.T) {
	m := connectionForm(t, 140)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: minimumHeight})
	focusField(t, m, "credential")
	m.render()
	m.cfg.Services = map[string]config.Service{}
	pane := &m.infoPane
	if !pane.more {
		t.Skip("the explanation fits the pane at this size")
	}
	wheelAt(t, m, pane.x0+3, pane.y0+3, tea.MouseWheelDown)
	if pane.view.offset() != wheelText {
		t.Errorf("the wheel moved the pane to %d, want %d", pane.view.offset(), wheelText)
	}
}
