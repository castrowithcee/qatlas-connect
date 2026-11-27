package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/charmbracelet/x/ansi"
)

// The export questions and the findings are kit lists in the workspace: the screen draws what stands above
// and below them in its dialog panel, the list takes the lines that are left, and its targets are the screen's.

// workList draws list in the panel between head and foot, which end in a newline, and places its targets.
func (m *Model) workList(list *kittui.List, head, foot string) string {
	p := m.dialogPanelOf()
	height := m.height - strings.Count(head, "\n") - strings.Count(foot, "\n") - 1
	view, targets := list.View(styles.Styles, p.room, max(height, 0))
	lines := strings.Split(view, "\n")
	// The list fills its height with blank lines; the frame closes below the last one it drew.
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	b.WriteString(head)
	for _, line := range lines {
		b.WriteString(p.side(line) + "\n")
	}
	m.addListTargets(targets, head, len(lines))
	b.WriteString(foot)
	return b.String()
}

// workspaceList is the kit list the screen shows in the workspace, or nil.
func (m *Model) workspaceList() *kittui.List {
	switch {
	case m.screen == screenFindings && m.findings != nil:
		return m.findings.list
	case m.screen == screenTargets && m.targetPick == nil && m.targetAdd != nil:
		return m.targetAdd.choices
	case m.screen == screenTargets && m.targetPick == nil && m.targetEdit < 0 && !m.labelEdit && !m.targetRemove &&
		m.targetList.list != nil:
		return m.targetList.list
	case m.screen == screenTransfer && m.export != nil && m.export.step == exportSelect:
		return m.export.list
	case m.screen == screenTransfer && m.export != nil && m.export.step == exportMode:
		return m.export.mode
	}
	return nil
}

// clickWorkList is a click on a target of the workspace list: it marks a row or ticks a box, and a click on
// the marked row is a press of enter, so it meets every check the key meets.
func (m *Model) clickWorkList(list *kittui.List, r kittui.MouseResult) tea.Cmd {
	if r.Item == "" && r.Action != actSearch {
		return nil
	}
	if open, _ := list.Click(r); open == "" {
		return nil
	}
	return m.press(tea.KeyPressMsg{Code: tea.KeyEnter})
}
