package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// valuePicker is the kit's list of the values of the focused row, open over the form. Its entry IDs are the
// indexes of choices, since a value may hold what an ID may not. A row that ticks keeps the ticks in
// m.pickerMarks, which the row takes over only when they are kept.
type valuePicker struct {
	list    *kittui.List
	choices []string
}

func (p *valuePicker) choice(id kittui.ItemID) string {
	i, err := strconv.Atoi(string(id))
	if err != nil || i < 0 || i >= len(p.choices) {
		return ""
	}
	return p.choices[i]
}

func (p *valuePicker) selected() (string, bool) {
	id, ok := p.list.Selected()
	return p.choice(id), ok
}

// valuePicked reports whether f takes its values from the kit's list: a choice, the effects and the models
// of a token.
func valuePicked(f field) bool {
	return f.kind == fieldChoice || f.kind == fieldMultiChoice || f.kind == fieldToolList && f.label == vorbilderLabel
}

// openValuePicker opens the values of the focused row over the form, on the value in force or the ticks of
// the row, with the filter ready for typing.
func (m *Model) openValuePicker() {
	f := m.fields[m.focus]
	if len(f.choices) == 0 {
		return
	}
	ticks := f.kind != fieldChoice
	m.pickerMarks, m.pickerGroups, m.pickerEffects = nil, nil, nil
	columns := []kittui.Column{{Title: columnID("value")}}
	if !ticks {
		columns = append(columns, kittui.Column{Title: columnID("in force"),
			Tone: func(string) kittui.TextStyle { return kittui.StyleOK }})
	}
	items := make([]kittui.ListItem, len(f.choices))
	current, spaced := "", false
	for i, choice := range f.choices {
		shown := m.logText(m.choiceShown(choice))
		spaced = spaced || strings.ContainsRune(shown, ' ')
		id := kittui.ItemID(strconv.Itoa(i))
		cells := []string{shown}
		if !ticks {
			mark := ""
			if choice == f.value() {
				current, mark = string(id), tr(tFormCurrent)
			}
			cells = append(cells, mark)
		}
		items[i] = kittui.ListItem{ID: id, Cells: cells}
	}
	enter := hintLabelID("{enter} choose")
	if ticks {
		enter = hintLabelID("{enter} keep")
	}
	spec := kittui.ListSpec{Columns: columns, Keys: listKeys(""), Search: kittui.SearchAsTyped, Wrap: true,
		Empty: tPickNoValue, Title: listTitle, CloseOutside: true, Buttons: []kittui.FormButton{
			{Action: actEnter, Label: enter, Primary: true}, {Action: actBack, Label: hintLabelID("{back} cancel")}}}
	if ticks {
		// Space ticks while typing only where no value holds a space, so every value stays searchable.
		spec.Keys.Toggle, spec.Ticks, spec.TickWhileTyping = actSpace, true, !spaced
	}
	list, err := kittui.NewList(spec, keymap, titled{translator(false), tr(tFormChooseRow, m.shownLabel(f.label))})
	if err == nil {
		err = list.SetItems(items)
	}
	if err == nil && ticks {
		m.pickerMarks = map[string]bool{}
		var ids []kittui.ItemID
		for i, choice := range f.choices {
			m.pickerMarks[choice] = f.selected[choice]
			if f.selected[choice] {
				ids = append(ids, kittui.ItemID(strconv.Itoa(i)))
			}
		}
		err = list.SetTicked(ids)
	}
	if err != nil {
		m.pickerMarks, m.fail = nil, err.Error()
		return
	}
	list.Select(kittui.ItemID(current))
	m.valuePick = &valuePicker{list: list, choices: f.choices}
	m.clearMessages()
	m.showValuePicker()
}

func (m *Model) showValuePicker() {
	m.showModal(&listModal{List: m.valuePick.list, keys: []action{actInfo}, after: m.syncTicks}, m.valueAnswer)
}

// valueAnswer runs what the value picker answers. Only enter changes the row: it takes the marked value, or
// keeps the ticks. Back leaves the row as it is and asks first where ticks changed.
func (m *Model) valueAnswer(act action) tea.Cmd {
	p := m.valuePick
	switch act {
	case actEnter:
		choice, ok := p.selected()
		m.closeValuePicker()
		if m.pickerMarks != nil {
			m.keepPicker()
		} else if ok {
			return m.choose(choice)
		}
	case actBack:
		if m.pickerChanged() {
			m.suspendValuePicker()
			m.askLeave(false)
			return nil
		}
		m.closeValuePicker()
	case actInfo:
		if s := m.pickerSubject(); !s.empty() {
			m.openExplanation(s)
		}
	}
	return nil
}

func (m *Model) closeValuePicker() {
	m.closeModal()
	m.valuePick = nil
}

// suspendValuePicker hides the value picker for the leave question, which treats it as screenPicker; the
// picker comes back with resumeValuePicker when the question returns there.
func (m *Model) suspendValuePicker() {
	for m.modal != nil {
		m.closeModal()
	}
	m.screen = screenPicker
}

func (m *Model) resumeValuePicker() {
	if m.screen == screenPicker && m.valuePick != nil {
		m.screen = screenForm
		m.showValuePicker()
	}
}

// syncTicks takes the ticks of the value picker into m.pickerMarks by the rules of toggleMark, so in the
// effects a tick on default drops the others and one beside it drops default; the list then shows the result.
func (m *Model) syncTicks() {
	p := m.valuePick
	if p == nil || m.pickerMarks == nil {
		return
	}
	now := map[string]bool{}
	for _, id := range p.list.Ticked() {
		now[p.choice(id)] = true
	}
	var changed []string
	for _, choice := range p.choices {
		if now[choice] != m.pickerMarks[choice] {
			changed = append(changed, choice)
		}
	}
	permissions := m.fields[m.focus].kind == fieldMultiChoice
	for _, choice := range changed {
		toggleMark(m.pickerMarks, choice, permissions)
	}
	var ids []kittui.ItemID
	for i, choice := range p.choices {
		if m.pickerMarks[choice] {
			ids = append(ids, kittui.ItemID(strconv.Itoa(i)))
		}
	}
	_ = p.list.SetTicked(ids)
}
