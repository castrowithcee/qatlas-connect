package tui

import (
	"bytes"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Tab runs through every field of the bar and then leaves it for the rows, and esc on the bar goes to the
// navigation, in every navigation form; All has no date field.
func TestLogBarTabRunsThroughEveryField(t *testing.T) {
	f := activityLog(t)
	for _, form := range PrefsSchema().Navigations {
		t.Run(form, func(t *testing.T) {
			m := f.model(false, f.localToday())
			m.prefs.Navigation = form
			m = sizedModel(m, 180, 30)
			press(t, m, "f")
			for _, c := range []struct {
				mode   logMode
				fields int
			}{{logModeDay, 8}, {logModeAll, 7}} {
				mode, fields := c.mode, c.fields
				if mode == logModeAll {
					pump(t, m, "right", "right")
				}
				if m.logs.mode != mode {
					t.Fatalf("mode %v, want %v", m.logs.mode, mode)
				}
				for range fields - 1 {
					press(t, m, "tab")
					if m.logs.focus != logFocusBar {
						t.Fatalf("%v: tab left the bar before its last field", mode)
					}
				}
				press(t, m, "tab")
				if m.logs.focus != logFocusRows {
					t.Fatalf("%v: tab on the last field = focus %v, want the rows", mode, m.logs.focus)
				}
				press(t, m, "shift+tab")
				for range fields - 1 {
					press(t, m, "shift+tab")
				}
				if m.logs.focus != logFocusBar || !strings.Contains(m.render(), ">  Mode:") {
					t.Fatalf("%v: shift+tab did not come back to the mode", mode)
				}
			}
			press(t, m, "esc")
			if m.screen != screenNav {
				t.Errorf("esc on the bar = screen %v, want the navigation", m.screen)
			}
		})
	}
}

// A new mode loads its days, and a filter keeps its value over every load, also one whose rows no longer hold
// the value.
func TestLogBarFilterKeepsItsValueOverAReload(t *testing.T) {
	f := activityLog(t)
	m := f.wideModel(false, f.localToday())
	tool := logBarFilters[logFilterTool]
	// Mode, date, way, client, then the tool.
	press(t, m, "f", "tab", "tab", "tab", "tab", "enter")
	typeText(t, m, "list")
	press(t, m, "enter")
	if m.logs.filters[logFilterTool] != "wiki.pages.list" {
		t.Fatalf("the search of the values did not take wiki.pages.list: %v", m.logs.filters)
	}
	loads := m.logs.loadID
	m.logs.setFocus(logFocusRows, false)
	press(t, m, "f")
	pump(t, m, "right")
	if m.logs.mode != logModeRange || m.logs.loadID == loads || !m.logs.loaded {
		t.Fatalf("a new mode did not load: mode %v", m.logs.mode)
	}
	if m.logs.filters[logFilterTool] != "wiki.pages.list" || m.logs.bar.Value(tool) != "=wiki.pages.list" {
		t.Fatalf("the reload dropped the tool filter: %v, bar %q", m.logs.filters, m.logs.bar.Value(tool))
	}
	m.logs.mode, m.logs.day = logModeDay, "2001-01-01"
	m.syncLogBar()
	deliver(m, m.loadLogs())
	if len(m.logs.rows) != 0 || m.logs.bar.Value(tool) != "=wiki.pages.list" {
		t.Fatalf("a day without the value dropped the filter: rows %d, bar %q", len(m.logs.rows), m.logs.bar.Value(tool))
	}
}

// Where the fields do not fit, one button with the number of fields off their first value stands for them:
// enter opens the modal with all fields, enter takes every change and esc drops them.
func TestLogBarButtonOpensTheModalOfAllFields(t *testing.T) {
	f := activityLog(t)
	m := sizedModel(f.model(false, f.localToday()), 80, 24)
	if view := m.render(); !strings.Contains(view, "Filter (0)") {
		t.Fatalf("the narrow bar is no button:\n%s", view)
	}
	press(t, m, "f", "enter")
	if _, ok := m.modal.(*logBarModal); !ok || !strings.Contains(m.render(), "Result") {
		t.Fatalf("enter on the button did not open the modal:\n%s", m.render())
	}
	// The tool is the fifth field: mode, date, way, client, tool.
	press(t, m, "down", "down", "down", "down", "right", "esc")
	if m.modal != nil || m.logs.filters[logFilterTool] != "" {
		t.Fatalf("esc did not drop the change: modal %v filters %v", m.modal != nil, m.logs.filters)
	}
	press(t, m, "enter", "down", "down", "down", "down", "right", "enter")
	if m.modal != nil || m.logs.filters[logFilterTool] == "" || !strings.Contains(m.render(), "Filter (1)") {
		t.Fatalf("enter did not take the change: filters %v\n%s", m.logs.filters, m.render())
	}
	// The date in the modal opens the date form.
	press(t, m, "enter", "down", "enter")
	if logDateForm(m) == nil {
		t.Fatalf("the date of the modal did not open the date form:\n%s", m.render())
	}
}

// A value of the log is not trusted: a control sequence never reaches the bar or its values, a value that
// looks like a key placeholder or an ID of the catalog shows as it is, and no ID of a value holds a control
// character.
func TestLogBarShowsLogValuesMadeSafe(t *testing.T) {
	f := newLogFixture(t)
	f.append("mcp", "codex", "op.one", "wiki", "success")
	f.append("mcp", "codex", "op.two", "wiki", "success")
	f.edit(f.today(), func(lines [][]byte) [][]byte {
		lines[0] = bytes.Replace(lines[0], []byte(`"codex"`), []byte(`"\u001b[31mevil‮"`), 1)
		lines[0] = bytes.Replace(lines[0], []byte(`"op.one"`), []byte(`"{back}"`), 1)
		lines[0] = bytes.Replace(lines[0], []byte(`"wiki"`), []byte(`"LogAll"`), 1)
		return lines
	})
	m := f.wideModel(false, f.localToday())
	for _, k := range []int{logFilterClient, logFilterTool, logFilterConnection} {
		for _, c := range m.logs.bar.Fields()[k+2].Options {
			if strings.ContainsFunc(string(c.ID), unicode.IsControl) {
				t.Errorf("the value ID %q holds a control character", c.ID)
			}
		}
	}
	// Client, tool and connection are the fourth to sixth fields.
	for i, want := range map[int]string{3: "�[31mevil�", 4: "{back}", 5: "LogAll"} {
		m.logs.setFocus(logFocusRows, false)
		press(t, m, "f")
		for range i {
			press(t, m, "tab")
		}
		press(t, m, "enter")
		view := m.render()
		if strings.Contains(view, "\x1b[31mevil") || strings.Contains(view, "‮") {
			t.Errorf("a control sequence from the log reached the values: %q", view)
		}
		if !strings.Contains(ansi.Strip(view), want) {
			t.Errorf("the values do not show %q as it is:\n%s", want, ansi.Strip(view))
		}
		press(t, m, "esc")
	}
}

// The open values of a field take every key as search text; esc closes them and then leaves the bar.
func TestLogBarValuesTakeEveryKey(t *testing.T) {
	f := activityLog(t)
	m := f.wideModel(false, f.localToday())
	press(t, m, "f", "tab", "tab", "tab", "tab", "enter")
	typeText(t, m, "2q?:s")
	if m.section != sectionLogs || m.modal != nil || m.quitting || !m.logs.bar.Open() || m.logs.stats {
		t.Fatalf("a typed key acted: section %v modal %v quitting %v", m.section, m.modal != nil, m.quitting)
	}
	press(t, m, "esc")
	if m.logs.bar.Open() || m.logs.focus != logFocusBar {
		t.Fatalf("esc did not close the values alone: focus %v", m.logs.focus)
	}
	press(t, m, "esc")
	if m.screen != screenNav {
		t.Errorf("esc on the bar = screen %v, want the tab line", m.screen)
	}
}

// At every size the bar fits: its fields where they fit in one line, else the button.
func TestLogBarAtEverySize(t *testing.T) {
	f := activityLog(t)
	for _, size := range []struct {
		w, h   int
		button bool
	}{{40, 15, true}, {59, 19, true}, {80, 24, true}, {150, 42, false}} {
		m := sizedModel(f.model(false, f.localToday()), size.w, size.h)
		press(t, m, "f")
		view := m.render()
		assertViewFits(t, view, size.w, size.h)
		if button := strings.Contains(view, "Filter (0)"); button != size.button || button == strings.Contains(view, "Mode: Day") {
			t.Errorf("%dx%d: button %v, want %v:\n%s", size.w, size.h, button, size.button, view)
		}
	}
}
