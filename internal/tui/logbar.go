package tui

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// The fields of the bar of the Logs: the mode, the date as an action field, and one per filter.
const (
	logBarMode kit.FieldID = "mode"
	logBarDate kit.FieldID = "date"
)

// logBarFilters are the filter fields in the order of logFilterShort.
var logBarFilters = [logFilterCount]kit.FieldID{"way", "client", "tool", "connection", "effect", "result"}

// logChoiceAll is the value of a filter that keeps every row; a value of the log is "=" and the value, which
// is already safe to print and so holds no control character.
const logChoiceAll kit.ChoiceID = "all"

// logBar is the bar of the Logs, built on first use.
func (m *Model) logBar() *kit.FilterBar {
	if m.logs.bar == nil {
		m.syncLogBar()
	}
	return m.logs.bar
}

// syncLogBar builds the bar anew from the mode, the days and the filters in force: Day and Range have the
// date field, All has none. The focus of the bar goes to the mode, its first field, where the bar had it.
func (m *Model) syncLogBar() {
	lv := m.logs
	var modes []kit.Choice
	for mode := logModeDay; mode < logModeCount; mode++ {
		modes = append(modes, kit.Choice{ID: logModeChoice(mode), Label: kit.TextID([...]textID{tLogModeDay, tLogModeRange, tLogModeAll}[mode])})
	}
	fields := []kit.FilterField{{ID: logBarMode, Label: kit.TextID(tLogBarMode), Options: modes, Selected: logModeChoice(lv.mode)}}
	switch lv.mode {
	case logModeDay:
		fields = append(fields, kit.FilterField{ID: logBarDate, Label: kit.TextID(tLogBarDate), Action: actEnter, Text: lv.day})
	case logModeRange:
		fields = append(fields, kit.FilterField{ID: logBarDate, Label: kit.TextID(tLogBarRange), Action: actEnter, Text: lv.from + ".." + lv.to})
	}
	for k, id := range logBarFilters {
		fields = append(fields, kit.FilterField{ID: id, Label: kit.TextID(logFilterShort[k]), Options: m.logChoices(k),
			Selected: logValueChoice(lv.filters[k])})
	}
	bar, err := kit.NewFilterBar(kit.FilterBarSpec{Fields: fields, Keys: kit.FilterBarKeys{Left: actLeft, Right: actRight,
		Next: actNext, Prev: actPrev, Up: actUp, Down: actDown, Enter: actEnter, Back: actBack,
		PageUp: actPageUp, PageDown: actPageDown}}, keymap, translator(false))
	if err != nil {
		// Every value is made safe to print before it gets here; a bar the kit still refuses is a bug.
		panic("the bar of the Logs is invalid: " + err.Error())
	}
	if lv.bar != nil && lv.bar.Focused() {
		bar.Focus(false)
	}
	lv.bar = bar
}

func logModeChoice(mode logMode) kit.ChoiceID {
	return [...]kit.ChoiceID{"day", "range", "all"}[mode]
}

// logValueChoice is the value of a filter as a choice: "" keeps every row.
func logValueChoice(value string) kit.ChoiceID {
	if value == "" {
		return logChoiceAll
	}
	return kit.ChoiceID("=" + value)
}

// logChoices are the values of filter k: all, then every value the loaded rows hold for it and the value in
// force, which a filter keeps over a load that no longer holds it. The labels are text of the log.
func (m *Model) logChoices(k int) []kit.Choice {
	lv := m.logs
	seen := map[string]bool{"": true}
	var values []string
	for _, v := range append([]string{lv.filters[k]}, rowValues(lv.rows, k)...) {
		if !seen[v] {
			seen[v] = true
			values = append(values, v)
		}
	}
	sort.Strings(values)
	choices := []kit.Choice{{ID: logChoiceAll, Label: kit.TextID(tLogAll)}}
	for _, v := range values {
		// A value of the log is text, never looked up as a catalog ID or for key placeholders.
		choices = append(choices, kit.Choice{ID: logValueChoice(v), Text: m.logText(v)})
	}
	return choices
}

func rowValues(rows []logRow, k int) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.values[k]
	}
	return out
}

// setLogBarOptions hands the bar the values of the rows just loaded and the days they were loaded for.
func (m *Model) setLogBarOptions() {
	bar := m.logBar()
	for k, id := range logBarFilters {
		// A value of the log is safe to print, and the value in force is among the choices: the kit takes them.
		_ = bar.SetOptions(id, m.logChoices(k))
	}
}

// takeLogBar applies the fields of the bar that changed: a new mode loads its days, a filter shows the rows
// it keeps, and the date field, where it was pressed, opens the date form.
func (m *Model) takeLogBar(changed []kit.FilterField) tea.Cmd {
	lv := m.logs
	var cmd tea.Cmd
	for _, f := range changed {
		if f.ID == logBarMode {
			for mode := logModeDay; mode < logModeCount; mode++ {
				if logModeChoice(mode) == f.Selected {
					lv.mode = mode
				}
			}
			cmd = m.loadLogs()
			continue
		}
		for k, id := range logBarFilters {
			if f.ID == id {
				lv.filters[k] = strings.TrimPrefix(string(f.Selected), "=")
				if f.Selected == logChoiceAll {
					lv.filters[k] = ""
				}
			}
		}
		lv.applyFilters()
	}
	_, pressed := m.logBar().TakeAction()
	if cmd != nil {
		// The date field comes and goes with the mode.
		m.syncLogBar()
	}
	if pressed {
		m.openLogDateDialog()
	}
	if len(m.logBar().Hints()) > 0 {
		m.openLogBarModal()
	}
	return cmd
}

// updateLogBar gives a key to the focused bar. It reports false for a key the bar leaves to the screen.
func (m *Model) updateLogBar(key tea.KeyPressMsg) (tea.Cmd, bool) {
	changed, handled := m.logBar().Update(key)
	return m.takeLogBar(changed), handled
}

// clickLogBar is a click on the bar, as the keys reach it: on its free area it takes the focus to the bar,
// on a field it opens its values or, for the date, the date form, and on a value it takes it.
func (m *Model) clickLogBar(r kit.MouseResult) tea.Cmd {
	lead, ok := m.logBarGuard()
	if !ok {
		return lead
	}
	lv := m.logs
	if r.Item == "" {
		if lv.focus != logFocusBar {
			lv.setFocus(logFocusBar, false)
		}
		return lead
	}
	lv.setFocus(logFocusBar, false)
	changed, _ := m.logBar().Click(r)
	return tea.Batch(lead, m.takeLogBar(changed))
}

// openLogBarModal shows the modal of a bar too narrow for its fields over the screen: it takes every key
// and click until enter takes or back drops its changes.
func (m *Model) openLogBarModal() {
	p := &logBarModal{m: m}
	m.showModal(p, func(action) tea.Cmd {
		m.closeModal()
		return m.takeLogBar(p.changed)
	})
}

// logBarModal is the modal of the bar at the seam of the kit's modals; it answers back once it closed.
type logBarModal struct {
	m       *Model
	changed []kit.FilterField
}

func (p *logBarModal) answer(changed []kit.FilterField) (action, bool) {
	p.changed = append(p.changed, changed...)
	if len(p.m.logBar().Hints()) == 0 {
		return actBack, true
	}
	return "", true
}

func (p *logBarModal) Update(msg tea.KeyPressMsg) (action, bool) {
	changed, _ := p.m.logBar().Update(msg)
	return p.answer(changed)
}

func (p *logBarModal) Click(r kit.MouseResult) (action, bool) {
	changed, _ := p.m.logBar().Click(r)
	return p.answer(changed)
}

func (p *logBarModal) Wheel(int) {}

func (p *logBarModal) Overlay(s kit.Styles, screen string, width, height int, room kit.Rect) (string, []kit.Target, bool) {
	return p.m.logBar().ModalOverlay(s, screen, width, height, room)
}

func (p *logBarModal) View(s kit.Styles, width, height int) (string, []kit.Target) {
	return p.m.logBar().ModalView(s, width, height)
}

func (p *logBarModal) Hints() []kit.Hint { return p.m.logBar().Hints() }
