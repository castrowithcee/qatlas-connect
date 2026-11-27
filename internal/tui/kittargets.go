package tui

import (
	"slices"
	"strings"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The target list of a connection and its add menu are kit lists in the workspace. The entry IDs are indexes,
// since a target or a menu line may hold what an ID may not; every shown text stands through logText.

// targetItems are the entries of the open target list, in order, and the kit list that shows them.
type targetItems struct {
	all  []string
	list *kittui.List
}

// newTargetList is the kit list of the open target list: the target, its label and, where the provider
// declares exclusions, whether it is excluded. It stops at its ends and has no filter.
func (m *Model) newTargetList() *kittui.List {
	columns := []kittui.Column{{Title: columnID("TARGET")}, {Title: columnID("LABEL"), Flex: true, Dimmed: true}}
	if m.excludeAvailable() {
		columns = append(columns, kittui.Column{Title: columnID("STATE"), Tone: func(string) kittui.TextStyle { return kittui.StyleWarn }})
	}
	list, err := kittui.NewList(kittui.ListSpec{Columns: columns, Keys: listKeys(""), Search: kittui.SearchNone,
		Empty: tFormNothingConfiguredYet}, keymap, translator(false))
	if err != nil {
		panic("the list of targets is invalid: " + err.Error())
	}
	return list
}

// showTargets draws the entries again with their labels and exclusions, the marked row at index at, kept in range.
func (m *Model) showTargets(at int) {
	t := &m.targetList
	items := make([]kittui.ListItem, len(t.all))
	for i, target := range t.all {
		cells := []string{m.logText(target), m.logText(m.targetLabels[target])}
		if m.excludeAvailable() {
			state := ""
			if m.targetExcluded[target] {
				state = strings.TrimSpace(tr(tTargetExcludedMark))
			}
			cells = append(cells, state)
		}
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: cells}
	}
	if err := t.list.SetItems(items); err != nil {
		panic("the targets are invalid: " + err.Error())
	}
	t.list.SetEmpty(emptyTargets(m.targetMetadata()))
	if len(items) > 0 {
		t.list.Select(choiceID(min(max(at, 0), len(items)-1)))
	}
}

// targetSelected is the marked target with its index in the list.
func (m *Model) targetSelected() (target string, at int, ok bool) {
	at, ok = chosen(m.targetList.list, len(m.targetList.all))
	if !ok {
		return "", 0, false
	}
	return m.targetList.all[at], at, true
}

// newAddList is the kit list of the add menu and the builder. It filters as typed: every printable key belongs
// to the typed line.
func newAddList() *kittui.List {
	keys := listKeys("")
	keys.Search = ""
	list, err := kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{{Title: columnID("CHOICE")}},
		Keys: keys, Search: kittui.SearchAsTyped, Wrap: true, Empty: tTargetNothingToChooseTypeA}, keymap, translator(false))
	if err != nil {
		panic("the list of the add menu is invalid: " + err.Error())
	}
	return list
}

// showAdd draws the rows of the menu or the builder, the one named exact marked where there is one and the
// first shown row otherwise. The typed row carries the typed line as its search text, so the filter never hides it.
func (m *Model) showAdd(exact string) {
	a := m.targetAdd
	items := make([]kittui.ListItem, len(a.rows))
	for i, key := range a.rows {
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: []string{m.logText(a.text[key])}}
		if key == addTyped {
			items[i].SearchText = a.query()
		}
	}
	if err := a.choices.SetItems(items); err != nil {
		panic("the add menu is invalid: " + err.Error())
	}
	if i := slices.Index(a.rows, exact); exact != "" && i >= 0 && a.choices.Select(choiceID(i)) {
		return
	}
	if shown := a.choices.Shown(); len(shown) > 0 {
		a.choices.Select(shown[0])
	}
}

// query is the line typed in the menu or the builder, without the blanks around it.
func (a *targetAdd) query() string { return strings.TrimSpace(a.choices.Query()) }

// selected is the row key of the marked line.
func (a *targetAdd) selected() (string, bool) {
	i, ok := chosen(a.choices, len(a.rows))
	if !ok {
		return "", false
	}
	return a.rows[i], true
}
