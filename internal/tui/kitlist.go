package tui

import (
	"slices"
	"strings"
	"unicode"

	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// The list of each section is the kit's List: rows, columns, groups, folds, the filter and the click targets.
// The editor hands it the entries with their cells and search text, and draws the title, the frame, the filter
// bar, the detail pane and the notes around it. Each section keeps its own list, so its folds outlive a visit
// to another section.

// noProviderGroup titles the group of entries whose provider cannot be determined. It sorts last.
const noProviderGroup = "no provider"

// groupedSection reports whether the list of s is shown grouped by provider.
func groupedSection(s section) bool {
	return s == sectionServices || s == sectionCredentials || s == sectionConnections
}

// isGroupRow reports whether a name stands for a group row rather than an entry; marked names a group row so.
func isGroupRow(name string) bool { return strings.HasPrefix(name, "\x00") }

func groupRow(key string) string { return groupRowPrefix + key }

// sectionColumns are the columns of the list of each section, titled by their English key. The name leads,
// the free text of a section is the first to give way, and a column that only adds detail to the others may
// be left out of a narrow terminal.
var sectionColumns = map[section][]kit.Column{
	sectionServices: {{Title: "NAME"}, {Title: "BASE URL", Flex: true}},
	sectionCredentials: {{Title: "NAME"},
		{Title: "STORAGE", Optional: true, Dimmed: true, Short: map[string]string{
			placeKeyring: "keyring", placeEnv: "env", placePlaintext: "file", placeVault: "vault"}},
		{Title: "SECRETS", Flex: true}},
	sectionConnections: {{Title: "NAME"}, {Title: "STATE", Optional: true, Tone: connectionStateTone},
		{Title: "SERVICE"}, {Title: "EFFECTS", Optional: true, Dimmed: true},
		{Title: "TOOLS", Optional: true, Dimmed: true}, {Title: "TARGETS", Optional: true, Dimmed: true},
		{Title: "DESCRIPTION", Flex: true}},
	sectionDefaults: {{Title: "PROVIDER OR TOOL"}, {Title: "CONNECTION"}},
	sectionApprovals: {{Title: "CONNECTION"}, {Title: "KIND", Dimmed: true},
		{Title: "CHANGE", Flex: true}},
	sectionTokens: {{Title: "NAME"}, {Title: "EXPIRES", Dimmed: true}, {Title: "MODELS", Flex: true}},
}

// columnShown draws the values of a column in the language of the editor; the cells keep the stored value.
var columnShown = map[section]map[int]func(string) string{
	sectionCredentials: {1: func(place string) string { return valueText(storageLabel, place) }},
}

// connectionStateTone colours the state of a connection: active green, expired red, anything else (disabled)
// dimmed. The word stays in the cell.
func connectionStateTone(cell string) kit.TextStyle {
	switch {
	case strings.HasPrefix(cell, valueText(stateLabel, stateActive)):
		return kit.StyleOK
	case strings.HasPrefix(cell, valueText(stateLabel, expiredText)):
		return kit.StyleFail
	}
	return kit.StyleHint
}

// mainListKeys are the keys the lists of the sections read.
var mainListKeys = kit.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome,
	Last: actEnd, Left: actLeft, Right: actRight, Enter: actEnter, Search: actSearch, Back: actBack}

// catalogID is id where a catalog holds a text for it, else the English text itself, which a Translator
// returns as it is.
func catalogID(id textID, english string) textID {
	if _, ok := translator(false).Find(id); ok {
		return id
	}
	return textID(english)
}

// listSpec is the list of section s. A section without a list of its own (the Overview, Logs, a settings
// page) holds an empty one, so the state of the list reads the same on every screen.
func listSpec(s section) kit.ListSpec {
	cols := slices.Clone(sectionColumns[s])
	if len(cols) == 0 {
		cols = []kit.Column{{Title: "NAME"}}
	}
	for i := range cols {
		c := &cols[i]
		if show := columnShown[s][i]; show != nil && c.Short != nil {
			short := make(map[string]string, len(c.Short))
			for value, label := range c.Short {
				short[show(value)] = label
			}
			c.Short = short
		}
		c.Title = catalogID(columnID(string(c.Title)), string(c.Title))
	}
	return kit.ListSpec{Columns: cols, Keys: mainListKeys, Search: kit.SearchByKey, Empty: tFormNothingConfiguredYet,
		NoGroup: catalogID(wordID(noProviderGroup), noProviderGroup), SearchInBar: groupedSection(s)}
}

// listFor is the list of section s, made on first use and anew in another language, whose texts it holds, as
// its filter bar does.
func (m *Model) listFor(s section) *kit.List {
	if m.listLang != uiLang {
		m.lists, m.bars, m.listLang = nil, nil, uiLang
	}
	if l := m.lists[s]; l != nil {
		return l
	}
	l, err := kit.NewList(listSpec(s), keymap, translator(false))
	if err != nil {
		panic("the list of a section is invalid: " + err.Error())
	}
	if m.lists == nil {
		m.lists = map[section]*kit.List{}
	}
	m.lists[s] = l
	return l
}

// fillList reads the entries of the active section anew, through its filter bar where it has one, and hands
// them to its list. The filter, the folds and, where it still shows, the marked entry stay.
func (m *Model) fillList() {
	names := m.entryNames(m.section)
	if groupedSection(m.section) {
		names = m.barNames(m.section, names)
	}
	m.names = names
	m.syncList()
}

// syncList hands the list the rows of its entries as they read now, so a state that changed since they were
// read (a secret resolved, a connection expired) shows on the next frame. The cells never show a secret value
// and the search text holds none (see cells and describe).
func (m *Model) syncList() {
	m.list = m.listFor(m.section)
	grouped := groupedSection(m.section)
	var provider func(string) string
	if grouped {
		provider = m.providerOf(m.section)
	}
	items := make([]kit.ListItem, 0, len(m.names))
	for _, name := range m.names {
		if strings.IndexFunc(name, unicode.IsControl) >= 0 {
			// The list refuses every entry where one holds a control character; no checked name does.
			continue
		}
		cells := m.cells(name)
		for i, show := range columnShown[m.section] {
			cells[i] = show(cells[i])
		}
		it := kit.ListItem{ID: kit.ItemID(name), Cells: cells, SearchText: m.describe(name)}
		if grouped {
			it.Group = provider(name)
		}
		items = append(items, it)
	}
	empty := m.emptyHelp()
	if len(items) == 0 && m.listBarShown() {
		empty = tr(tFormNoEntryMatchesTheFilter)
	}
	if err := m.list.SetItems(items); err != nil {
		// A list the kit refuses is shown empty with the reason, never as the entries it held before.
		_ = m.list.SetItems(nil)
		empty = err.Error()
	}
	m.list.SetGrouped(grouped && !m.opts(m.section).flat)
	m.list.SetEmpty(empty)
	m.list.SetFocused(m.rowsFocused())
}

// resetList fills the list with the entries of the active section s and marks its first entry, below its
// group row, without a filter.
func (m *Model) resetList(s section) {
	m.blurBar()
	m.leaveDetail()
	if s == sectionApprovals || s == sectionOverview {
		// Agents add requests at any time, so a section that shows them reads the queue as it opens.
		m.refreshRequests()
	}
	m.fillList()
	m.list.Reset()
}

// rowsFocused reports whether the rows of the list have the focus: the list is the screen, and neither a
// field of the filter bar, the notice below the rows nor the detail pane has it. Only then does a click on
// the marked row open it.
func (m *Model) rowsFocused() bool {
	return m.screen == screenList && !m.barFocused() && !m.noticeFocused() && !m.detailFocused()
}

// marked is the marked row of the list: the name of an entry, or groupRow of the key of a group row.
func (m *Model) marked() (string, bool) {
	if id, ok := m.list.Selected(); ok {
		return string(id), true
	}
	if key, ok := m.list.SelectedGroup(); ok {
		return groupRow(key), true
	}
	return "", false
}

// listShowsRows reports whether the list shows any row; a filter may hide every one.
func (m *Model) listShowsRows() bool {
	_, ok := m.marked()
	return ok
}

// listSection reports whether the active section is a list of entries.
func (m *Model) listSection() bool {
	_, ok := sectionColumns[m.section]
	return ok
}

// providerOf is the provider group key of an entry of the grouped section s.
func (m *Model) providerOf(s section) func(name string) string {
	switch s {
	case sectionServices:
		return func(name string) string { return m.cfg.Services[name].Provider }
	case sectionCredentials:
		return func(name string) string { return m.credentialProvider(name, m.cfg.Credentials[name]) }
	}
	return func(name string) string { return m.cfg.Services[m.cfg.Connections[name].Service].Provider }
}
