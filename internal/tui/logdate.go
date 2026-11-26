package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// The fields of the date form of the Logs.
const (
	logFieldDay  kit.FieldID = "day"
	logFieldFrom kit.FieldID = "from"
	logFieldTo   kit.FieldID = "to"
)

// openLogDateDialog opens the form of the bar's date field over the Logs: one day, or from and to. The save
// key or its button takes the days, back or its button closes it without a question, since nothing is saved.
func (m *Model) openLogDateDialog() {
	lv := m.logs
	field := func(id kit.FieldID, label textID, day string) kit.FormField {
		value, _ := time.ParseInLocation(logDateLayout, day, time.Local)
		return kit.FormField{ID: id, Kind: kit.FieldDate, Label: kit.TextID(label), Hint: kit.TextID(tLogDateHint),
			Value: kit.FormValue{Time: value}, Date: kit.DateField{Now: m.timeNow}}
	}
	spec := kit.FormSpec{
		Keys: kit.FormKeys{Up: actUp, Down: actDown, Next: actNext, Prev: actPrev, PageUp: actPageUp,
			PageDown: actPageDown, Left: actLeft, Right: actRight, First: actHome, Last: actEnd, Enter: actEnter,
			Toggle: actSpace, Save: actSave, Back: actBack},
		Buttons:      []kit.FormButton{{Action: actSave, Label: kit.TextFilterApply, Primary: true}, {Action: actBack, Label: kit.TextKeyBack}},
		CloseOutside: true,
	}
	switch lv.mode {
	case logModeDay:
		spec.Title, spec.Fields = kit.TextID(tLogDayTitle), []kit.FormField{field(logFieldDay, tLogLabelDate, lv.day)}
	case logModeRange:
		spec.Title, spec.Fields = kit.TextID(tLogRangeTitle),
			[]kit.FormField{field(logFieldFrom, tLogLabelFrom, lv.from), field(logFieldTo, tLogLabelTo, lv.to)}
	default:
		return
	}
	f, err := kit.NewForm(spec, keymap, translator(false))
	if err != nil {
		m.fail = err.Error()
		return
	}
	m.clearMessages()
	m.showModal(f, func(act action) tea.Cmd {
		if act != actSave {
			m.closeModal()
			return nil
		}
		return m.applyLogDates(f)
	})
}

// applyLogDates takes the days of the open date form and loads them, or shows why it cannot: an empty field,
// or from after to. The form refuses a text that is no date itself.
func (m *Model) applyLogDates(f *kit.Form) tea.Cmd {
	lv, text := m.logs, translator(false)
	values := f.Values()
	var problems []kit.FormProblem
	days := map[kit.FieldID]string{}
	for _, id := range []kit.FieldID{logFieldDay, logFieldFrom, logFieldTo} {
		v, ok := values[id]
		if !ok {
			continue
		}
		if v.Time.IsZero() {
			problems = append(problems, kit.FormProblem{Field: id, Cause: text.Lookup(kit.TextFormDateFormat),
				NextStep: text.Lookup(kit.TextFormDateUseDate)})
			continue
		}
		days[id] = v.Time.Format(logDateLayout)
	}
	if from, to := days[logFieldFrom], days[logFieldTo]; len(problems) == 0 && from > to {
		problems = append(problems, kit.FormProblem{Field: logFieldTo, Cause: tr(tLogDateOrder)})
	}
	if len(problems) > 0 {
		f.SetProblems(problems)
		return nil
	}
	if day, ok := days[logFieldDay]; ok {
		lv.day = day
	} else {
		lv.from, lv.to = days[logFieldFrom], days[logFieldTo]
	}
	m.closeModal()
	return m.loadLogs()
}
