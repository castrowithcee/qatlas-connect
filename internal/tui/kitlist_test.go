package tui

import (
	"slices"

	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// listRowsOf are the rows the list of m shows, top to bottom: entries by name, group rows as groupRow of
// their key. It walks the rows with the list's own keys and leaves the mark where it was.
func listRowsOf(m *Model) []string {
	was, ok := m.marked()
	var rows []string
	m.list.Act(actHome)
	for {
		name, shown := m.marked()
		if !shown || (len(rows) > 0 && rows[len(rows)-1] == name) {
			break
		}
		rows = append(rows, name)
		m.list.Act(actDown)
	}
	if ok {
		selectRow(m, was)
	}
	return rows
}

// selectRow marks the shown row name, an entry or a group row, and reports whether it is shown. An entry of a
// folded group unfolds it.
func selectRow(m *Model, name string) bool {
	if !isGroupRow(name) {
		return m.list.Select(kit.ItemID(name))
	}
	m.list.Act(actHome)
	for {
		at, ok := m.marked()
		if at == name {
			return true
		}
		m.list.Act(actDown)
		if next, _ := m.marked(); !ok || next == at {
			return false
		}
	}
}

// listCursor is the place of the mark among the shown rows, -1 for none.
func listCursor(m *Model) int {
	name, ok := m.marked()
	if !ok {
		return -1
	}
	return slices.Index(listRowsOf(m), name)
}

// setListCursor marks the shown row at index i.
func setListCursor(m *Model, i int) {
	if rows := listRowsOf(m); i >= 0 && i < len(rows) {
		selectRow(m, rows[i])
	}
}

// listGrouped reports whether the list of m shows group rows.
func listGrouped(m *Model) bool { return slices.ContainsFunc(listRowsOf(m), isGroupRow) }

// groupFolded reports whether the list of m shows the group row of key without any of its entries.
func groupFolded(m *Model, key string) bool {
	rows := listRowsOf(m)
	of := m.providerOf(m.section)
	return slices.Contains(rows, groupRow(key)) &&
		!slices.ContainsFunc(rows, func(name string) bool { return !isGroupRow(name) && of(name) == key })
}

// setNames makes names the entries of the list of the active section, as if the configuration held them,
// and marks the first.
func setNames(m *Model, names ...string) {
	m.names = names
	m.syncList()
	m.list.Reset()
}
