package tui

import (
	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// providerTable is how every provider row is chosen: the kit's list of all the providers the row offers, with
// their name and ID, the one in force marked, and a search typed at once. Whatever the number of providers,
// it is the same list, opened the same way, so finding it takes no knowledge of a hidden key.
//
// It holds no provider knowledge of its own: the rows are the values of the form row, and the names come
// from the provider metadata the configuration core hands out.
type providerTable struct {
	list *kittui.List
	// ids are the offered provider IDs, by the index that is the entry ID: the empty choice is no ID.
	ids []string
}

// selected is the marked provider.
func (t *providerTable) selected() (string, bool) {
	i, ok := chosen(t.list, len(t.ids))
	if !ok {
		return "", false
	}
	return t.ids[i], true
}

// providerName is how a provider reads: its name, or the empty value that a credential without a provider
// holds.
func (m *Model) providerName(id string) string {
	if id == "" {
		return choiceText(id)
	}
	if metadata, _ := m.cfg.ProviderMetadata(id); metadata.Name != "" {
		return metadata.Name
	}
	return id
}

// openProviderTable opens the table of the focused provider row on the provider in force. In the guided
// setup the table is the whole provider step, so enter goes on to the next step and esc cancels the setup.
func (m *Model) openProviderTable() {
	f := m.fields[m.focus]
	if len(f.choices) == 0 {
		return
	}
	columns := []kittui.Column{{Title: columnID("NAME")}, {Title: columnID("ID"), Dimmed: true},
		{Title: columnID("in force"), Tone: func(string) kittui.TextStyle { return kittui.StyleOK }}}
	items := make([]kittui.ListItem, len(f.choices))
	current := 0
	for i, id := range f.choices {
		shown, mark := "-", ""
		if id != "" {
			shown = m.logText(id)
		}
		if id == f.value() {
			current, mark = i, tr(tFormCurrent)
		}
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: []string{m.logText(m.providerName(id)), shown, mark}}
	}
	title, enter, back := tr(tProviderChooseProvider), "{enter} choose", "{back} cancel"
	if w := m.wizard; w != nil {
		title, enter, back = tr(tProviderSetupStepOfChooseProvider, w.step+1, setupSteps), "{enter} next", "{back} cancel setup"
	}
	list, err := newChoice(kittui.ListSpec{Columns: columns, Search: kittui.SearchAsTyped, Wrap: true,
		Empty: tProviderNoProviderMatchesEscKeeps}, title, items, current, enter, back)
	if err != nil {
		m.fail = err.Error()
		return
	}
	m.providers = &providerTable{list: list, ids: f.choices}
	m.screen = screenProviders
	m.clearMessages()
	m.syncChoice()
}

// providerAnswer runs what the table answers. Only enter changes the row, and with it every row that depends
// on it; back leaves them as they were.
func (m *Model) providerAnswer(act action) tea.Cmd {
	if m.providers == nil {
		return nil
	}
	switch act {
	case actEnter:
		id, ok := m.providers.selected()
		if !ok {
			return nil
		}
		m.providers, m.screen = nil, screenForm
		cmd := m.choose(id)
		if m.wizard != nil {
			m.setupNext()
		}
		return cmd
	case actBack:
		if m.wizard != nil {
			// The leave question it may ask returns to the table.
			return m.setupEsc()
		}
		m.providers, m.screen = nil, screenForm
	case actInfo:
		if s := m.providerSubject(); !s.empty() {
			m.openExplanation(s)
		}
	}
	return nil
}

// providerValue is how a provider row of the form reads: the name and ID of the provider it holds, cut
// to what the terminal leaves beside the label; its field ends in the arrow that shows it is opened.
func (m *Model) providerValue(f field) string {
	if len(f.choices) == 0 {
		return emptyValue(wordNone)
	}
	id := f.value()
	text := choiceText(id)
	if metadata, ok := m.cfg.ProviderMetadata(id); ok && id != "" {
		text = metadata.Name + " (" + id + ")"
	}
	if m.width > 0 {
		// The arrow of the field ends the row.
		text = truncateCells(text, m.valueRoom(f.label)-2)
	}
	return text
}

// providerHint puts the way into the table in front of what the row decides, so the row says itself how
// it is chosen.
func providerHint(f field) string {
	providers := 0
	for _, choice := range f.choices {
		if choice != "" {
			providers++
		}
	}
	lead := tr(tProviderEnterOpensAllProviders, providers)
	if f.hint == "" {
		return lead
	}
	return lead + "; " + f.hint
}
