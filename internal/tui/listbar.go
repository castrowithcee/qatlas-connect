package tui

import (
	"sort"
	"strings"

	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// The filter bar of the grouped lists (Connections, Services, Credentials) changes only what the list shows
// and in which order. It never touches the configuration, and it lives as long as the editor does.

// listOptions are the values of the filter bar of one section. The zero value is the default view: grouped
// by provider, every state, sorted by name.
type listOptions struct {
	flat       bool
	state      int
	byProvider bool
}

// barKind names a field of the filter bar.
type barKind int

const (
	barGroup barKind = iota
	barState
	barSort
)

// listStateValues are the alternatives of the state field; see connectionActive.
var listStateValues = []string{"All", "Active", "Inactive"}

// barKinds are the fields of the bar of section s: the state field only where the entries have a state.
func barKinds(s section) []barKind {
	if s == sectionConnections {
		return []barKind{barGroup, barState, barSort}
	}
	return []barKind{barGroup, barSort}
}

// listBarShown reports whether the list of the active section has a filter bar: a grouped section that
// holds any entry at all, so a bar that hides everything can still be changed back.
func (m *Model) listBarShown() bool {
	if !groupedSection(m.section) {
		return false
	}
	return len(m.entryNames(m.section)) > 0
}

// opts is the filter bar state of section s.
func (m *Model) opts(s section) *listOptions {
	if m.listOpts == nil {
		m.listOpts = map[section]*listOptions{}
	}
	if m.listOpts[s] == nil {
		m.listOpts[s] = &listOptions{}
	}
	return m.listOpts[s]
}

// barField is what one field of the bar reads: label, alternatives and the active one.
func (m *Model) barField(k barKind) (label string, values []string, active int) {
	o := m.opts(m.section)
	switch k {
	case barGroup:
		return "Group", []string{"Provider", "None"}, boolIndex(o.flat)
	case barState:
		return "State", listStateValues, o.state
	}
	return "Sort", []string{"Name", "Provider"}, boolIndex(o.byProvider)
}

func boolIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}

// setBar gives field k the alternative next.
func (m *Model) setBar(k barKind, next int) {
	o := m.opts(m.section)
	switch k {
	case barGroup:
		o.flat = next == 1
	case barState:
		o.state = next
	case barSort:
		o.byProvider = next == 1
	}
	m.relist()
}

// relist rebuilds the list after the bar changed, on the entry that was selected where it is still shown.
func (m *Model) relist() {
	name, ok := m.selected()
	m.fillList()
	if ok {
		m.list.Select(kit.ItemID(name))
	}
}

// connectionActive reports whether the connection offers its tools: not disabled and not expired.
func (m *Model) connectionActive(name string) bool {
	return strings.HasPrefix(m.connectionStateText(m.cfg.Connections[name]), stateActive)
}

// barNames applies the bar of section s to its sorted entry names: the state filter and the sort.
func (m *Model) barNames(s section, names []string) []string {
	o := m.opts(s)
	if s == sectionConnections && o.state != 0 {
		kept := names[:0:0]
		for _, name := range names {
			if m.connectionActive(name) == (o.state == 1) {
				kept = append(kept, name)
			}
		}
		names = kept
	}
	if o.byProvider {
		of := m.providerOf(s)
		sort.SliceStable(names, func(i, j int) bool {
			a, b := of(names[i]), of(names[j])
			if (a == "") != (b == "") {
				return b == ""
			}
			return a < b
		})
	}
	return names
}

// searchContent is what the search field of a filter list shows: "/ " and the query, or the input being
// typed, and nothing while the search is idle, which shows the placeholder.
func searchContent(l *filterList, room int) string {
	switch {
	case l.editing:
		return keyLabel(actSearch) + " " + inputView(fitInput(l.input, max(room-3, 1)))
	case l.query() != "":
		return keyLabel(actSearch) + " " + truncateCells(l.query(), max(room-3, 1))
	}
	return ""
}
