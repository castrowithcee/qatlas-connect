package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// configBytes is the content of the configuration file, or a marker where it does not exist yet.
func configBytes(m *Model) string {
	if m.store == nil {
		return fmt.Sprintf("%#v", *m.cfg)
	}
	b, err := os.ReadFile(m.store.Path())
	if err != nil {
		return "absent"
	}
	return string(b)
}

// Where the width does not suffice the value of a field of the Logs bar is cut with an ellipsis, and label and
// arrow stay.
func TestDropdownFieldCutsItsValue(t *testing.T) {
	got := ansi.Strip(dropdownField("Group", "Provider", false, 14))
	if lipgloss.Width(got) > 14 || !strings.Contains(got, "Group: ") || !strings.Contains(got, "…") || !strings.Contains(got, "▾") {
		t.Errorf("field = %q", got)
	}
}

// Activity: enter opens the list of the mode, the tabs change with left and right, the date keeps its dialog, and
// a click on a filter field opens its picker.
func TestActivityDropdowns(t *testing.T) {
	f := newLogFixture(t)
	f.append("mcp", "codex", "wiki.pages.get", "wiki", "success")
	m := f.model(true, f.localToday())

	press(t, m, "tab", "tab", "right")
	if m.dd != nil || m.logs.mode != logModeRange {
		t.Fatalf("right on Mode: open %v mode %v", m.dd != nil, m.logs.mode)
	}
	press(t, m, "enter")
	if m.dd == nil {
		t.Fatal("enter on Mode did not open its list")
	}
	press(t, m, "esc")
	if m.dd != nil || m.logs.mode != logModeRange {
		t.Fatalf("esc: open %v mode %v", m.dd != nil, m.logs.mode)
	}
	press(t, m, "enter", "down", "enter")
	if m.dd != nil || m.logs.mode != logModeAll {
		t.Fatalf("enter did not take All: mode %v", m.logs.mode)
	}
	press(t, m, "shift+tab", "right")
	if m.dd != nil || m.logs.focus != logFocusTabs || !m.logs.heat {
		t.Error("right on the tabs did not show the heatmap")
	}

	clickOn(t, m, "Client: all")
	if m.logs.pick == nil || m.logs.pick.filter != logFilterClient {
		t.Fatalf("a click on the Client field did not open its picker")
	}
}
