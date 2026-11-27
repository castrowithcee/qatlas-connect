package tui

import (
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-kit/projectpath"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The path list of a connection is edited in a screen of its own, opened from its row like the target list:
// a adds a path, enter or e edits the selected one, and d removes it after asking. F2 keeps the list and
// saves the form in one step, from every state of the screen, the typed path included. esc is one level back:
// it closes an unchanged list, cancels an unchanged entry, and asks about a changed list or a typed entry,
// so no change is dropped silently.
//
// A path is typed into the kit's dialog, which suggests the subdirectories of what is typed up to its last
// separator, never files; with nothing typed yet, the directory the TUI was started in is the one suggestion. A path is checked by the rules of the core when it is taken, and the list as a
// whole when the form is saved. Whether the directory exists is not checked: a path is only resolved when a
// call runs, and config validate warns about one that names nothing.

// pathChoice is the open path list: the paths as they stand, and the kit's list that shows them by the index
// that is the entry ID.
type pathChoice struct {
	list *kittui.List
	all  []string
}

// selected is the marked path.
func (p *pathChoice) selected() (string, bool) {
	i, ok := chosen(p.list, len(p.all))
	if !ok {
		return "", false
	}
	return p.all[i], true
}

// cursor is the index of the marked path.
func (p *pathChoice) cursor() int {
	i, _ := chosen(p.list, len(p.all))
	return i
}

// set shows entries with the mark on the entry at index.
func (p *pathChoice) set(entries []string, index int) {
	items := make([]kittui.ListItem, len(entries))
	for i, path := range entries {
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: []string{path}}
	}
	// The items have index IDs and one cell each, which SetItems accepts.
	_ = p.list.SetItems(items)
	p.all = entries
	p.list.Select(choiceID(min(index, len(entries)-1)))
}

// pathListKeys are the keys the path list reads; its letters are the actions of the screen, so it has no search.
var pathListKeys = kittui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome,
	Last: actEnd, Left: actLeft, Right: actRight, Enter: actEnter, Back: actBack}

// openPaths opens the entries of the focused path list on its first path. A path is a text of the
// configuration; the kit draws its control characters as escapes.
func (m *Model) openPaths() {
	empty := tPathNoneEveryProject
	if isFilesLabel(m.fields[m.focus].label) {
		empty = tPathNoneReleased
	}
	list, err := kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{{Title: columnID("Path")}},
		Keys: pathListKeys, Search: kittui.SearchNone, Empty: empty}, keymap, translator(false))
	if err != nil {
		m.fail = err.Error()
		return
	}
	m.pathList = &pathChoice{list: list}
	m.pathList.set(append([]string(nil), m.fields[m.focus].entries...), 0)
	m.pathEdit, m.pathRemove = -1, false
	m.screen = screenPaths
	m.clearMessages()
}

// updatePaths handles the path list screen. The typed path and the remove question are dialogs; see pathAsk
// and pathRemoveAsk.
func (m *Model) updatePaths(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case keyIs(key, actSave):
		// F2 saves from every state of the list, the way it does from every row of the form. A typed path
		// is taken first; one the rules refuse stays typed with the reason, and nothing is saved.
		if m.pathEdit >= 0 {
			m.takePath()
			if m.pathEdit >= 0 {
				return nil
			}
		}
		m.keepPaths()
		return m.updateForm(key)
	}
	path, selected := m.pathList.selected()
	switch {
	case keyIs(key, actBack):
		return m.leavePaths()
	case keyIs(key, actNew):
		m.editPath(len(m.pathList.all), "")
	case keyIs(key, actEnter) || keyIs(key, actEdit):
		if !selected {
			// An empty list has nothing to edit, so enter adds its first path.
			m.editPath(len(m.pathList.all), "")
			return nil
		}
		m.editPath(m.pathList.cursor(), path)
	case keyIs(key, actDelete):
		if selected {
			m.pathRemove = true
			m.clearMessages()
		}
	default:
		m.pathList.list.Update(key)
	}
	return nil
}

// keepPaths hands the list over to its row and returns to the form. Nothing is written yet.
func (m *Model) keepPaths() {
	m.fields[m.focus].entries = append([]string(nil), m.pathList.all...)
	m.screen = screenForm
	m.clearMessages()
}

// leavePaths closes the list without keeping it. An unchanged list closes at once; a changed one asks
// first, like a changed form does.
func (m *Model) leavePaths() tea.Cmd {
	m.clearMessages()
	if slices.Equal(m.pathList.all, m.fields[m.focus].entries) {
		m.screen = screenForm
		return nil
	}
	m.askLeave(false)
	return nil
}

// pathTyped reports whether the path being typed differs from the one it started from.
func (m *Model) pathTyped() bool {
	original := ""
	if m.pathEdit < len(m.pathList.all) {
		original = m.pathList.all[m.pathEdit]
	}
	return m.pathValue != original
}

// pathsPending reports whether closing the screen for good would lose a changed list or a typed path.
func (m *Model) pathsPending() bool {
	if m.pathEdit >= 0 && m.pathTyped() {
		return true
	}
	return !slices.Equal(m.pathList.all, m.fields[m.focus].entries)
}

// editPath starts typing the path at index, or a new one at the end of the list.
func (m *Model) editPath(index int, value string) {
	m.dropQuestion()
	m.pathEdit, m.pathValue = index, value
	m.clearMessages()
}

// takePath puts the typed path into the list once the rules of the core accept it and the list does not
// name its directory yet. A refused path stays typed with the reason. A trailing separator, which every
// suggestion ends in, is not kept.
func (m *Model) takePath() {
	value := strings.TrimSpace(m.pathValue)
	for len(value) > 1 && os.IsPathSeparator(value[len(value)-1]) {
		value = value[:len(value)-1]
	}
	m.status = ""
	if value == "" {
		m.fail = tr(tPathAPathMustNotBe)
		return
	}
	if isFilesLabel(m.fields[m.focus].label) {
		if err := config.CheckFilesEntry(value); err != nil {
			m.fail = tr(tPathDirectory, err.Error())
			return
		}
	} else if err := config.CheckPath(value); err != nil {
		m.fail = tr(tPathPath, err.Error())
		return
	}
	entries := append([]string(nil), m.pathList.all...)
	for i, entry := range entries {
		if i != m.pathEdit && config.SamePath(entry, value) {
			m.fail = tr(tPathThisDirectoryIsAlreadyIn)
			return
		}
	}
	if m.pathEdit >= len(entries) {
		entries = append(entries, value)
	} else {
		entries[m.pathEdit] = value
	}
	index := m.pathEdit
	m.pathList.set(entries, index)
	m.pathEdit = -1
	m.clearMessages()
}

// pathRemoveAsk asks before the selected path leaves the list.
func (m *Model) pathRemoveAsk() ask {
	path, _ := m.pathList.selected()
	// %q keeps control characters visible, but it would also double every backslash of a Windows path.
	quoted := strings.ReplaceAll(fmt.Sprintf("%q", path), `\\`, `\`)
	return ask{title: tAskRemovePath, question: tr(tPathRemoveFromTheList, quoted), consequence: tr(tAskListEntryLeaves),
		answers: yesNo("{yes} remove", "{no}/{back} keep"), keep: actNo, danger: actYes, save: true}
}

// answerPathRemove answers the remove question: y takes the path out of the list, n and esc keep it, and F2
// keeps it and saves as from the list.
func (m *Model) answerPathRemove(act action) tea.Cmd {
	switch act {
	case actYes:
		m.removePath()
		return nil
	case actSave:
		m.pathRemove = false
		save, _ := keyOf(actSave)
		return m.updatePaths(save)
	}
	m.pathRemove = false
	m.status = tr(tPathPathKept)
	return nil
}

// removePath takes the selected path out of the list.
func (m *Model) removePath() {
	m.pathRemove = false
	path, ok := m.pathList.selected()
	if !ok {
		return
	}
	i := m.pathList.cursor()
	entries := append(append([]string(nil), m.pathList.all[:i]...), m.pathList.all[i+1:]...)
	m.pathList.set(entries, i)
	m.status = tr(tPathRemoved, path)
}

// pathsView draws the path list screen: above the list what it does, the kit's list in the lines that are
// left, and below them the keys and the notes.
func (m *Model) pathsView() string {
	p := m.dialogPanelOf()
	title, hint := tr(tPathPaths), m.pathHint()
	if label := m.fields[m.focus].label; isFilesLabel(label) {
		title = tr(tPathDirectoriesFor, labelText(label))
	}
	if name := m.fieldValue("name"); name != "" {
		title += tr(tPathOf, name)
	}
	header := p.top(title, m.pathList.list.Position()) + p.text(styles.hint, hint) + p.blank()
	footer := p.bottom() + m.keyHint(m.pathKeys()) + m.notes()
	height := m.height - strings.Count(header, "\n") - strings.Count(footer, "\n") - 1
	view, targets := m.pathList.list.View(styles.Styles, p.room, max(height, 0))
	lines := strings.Split(view, "\n")
	// The list fills its height with blank lines; the frame closes below the last one it drew.
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	b.WriteString(header)
	for _, line := range lines {
		b.WriteString(p.side(line) + "\n")
	}
	m.addListTargets(targets, header, len(lines))
	b.WriteString(footer)
	return b.String()
}

// clickPaths is a click on the path list: it marks a path, and a click on the marked one acts as enter.
func (m *Model) clickPaths(r kittui.MouseResult) tea.Cmd {
	if r.Item == "" && r.Action == "" {
		return nil
	}
	if open, _ := m.pathList.list.Click(r); open == "" {
		return nil
	}
	return m.press(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// pathKeys is the key text of the path list screen; the path being typed names its keys in its dialog.
func (m *Model) pathKeys() string {
	if m.pathEdit >= 0 {
		return ""
	}
	return "{new} new · {enter}/{edit} edit · {delete} remove · {up}/{down} move · {save} save · {back} close · {command} cmds · {help} help"
}

// pathHint is what the paths of the focused list are for.
func (m *Model) pathHint() string {
	switch m.fields[m.focus].label {
	case filesReadLabel:
		return tr(tFormFilesReadHint)
	case filesWriteLabel:
		return tr(tFormFilesWriteHint)
	}
	return tr(tFormPathsHint)
}

// pathAsk is the dialog the path being added or edited is typed into, with the directories that complete it.
func (m *Model) pathAsk() ask {
	title := tPathAddTitle
	switch dir, edit := isFilesLabel(m.fields[m.focus].label), m.pathEdit < len(m.pathList.all); {
	case dir && edit:
		title = tPathEditDirTitle
	case dir:
		title = tPathAddDirTitle
	case edit:
		title = tPathEditTitle
	}
	return ask{title: title, question: m.pathHint(), input: true, value: m.pathValue,
		suggest: projectpath.Directories, keep: actEnter,
		answers: []kittui.Answer{{Action: actEnter, Label: hintLabelID("{enter} take")},
			{Action: actSave, Label: hintLabelID("{save} save")},
			{Action: actBack, Label: hintLabelID("{back} cancel entry")}}}
}

// answerPath answers the dialog of the typed path: enter takes it into the list, F2 takes it and saves as from
// the list, and esc cancels the entry, asking first once something was typed. A path the rules refuse stays
// typed with the reason.
func (m *Model) answerPath(act action) tea.Cmd {
	switch act {
	case actEnter:
		m.takePath()
	case actSave:
		save, _ := keyOf(actSave)
		return m.updatePaths(save)
	case actBack:
		if m.pathTyped() {
			m.askLeave(false)
			return nil
		}
		m.pathEdit = -1
		m.clearMessages()
	}
	return nil
}
