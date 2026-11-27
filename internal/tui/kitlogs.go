package tui

import (
	"strconv"

	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// The rows of the Logs are the kit's List: the columns, the search, the marked row, the position and the click
// targets. An entry stands for one row of the loaded log by its index, never by a value of the log, and holds
// the cells and the search text logRowOf made safe to print.

// logListColumns are the columns of the log table. The status leads and is never cut; the tool is the free
// text; the duration goes first in a narrow terminal, then the size, the connection, and the client.
var logListColumns = []kit.Column{
	{Title: columnID("status"), Tone: logStatusTone}, {Title: columnID("time"), Dimmed: true},
	{Title: columnID("way"), Dimmed: true}, {Title: columnID("client"), Optional: true},
	{Title: columnID("tool"), Flex: true}, {Title: columnID("connection"), Optional: true},
	{Title: columnID("result"), Tone: logResultTone}, {Title: columnID("resp."), Optional: true, Dimmed: true},
	{Title: columnID("dur."), Optional: true, Right: true},
}

// logStatusTone styles the status of a row by what it says; the word stays in the cell. Unverified is the
// normal state of an unsigned log, so its rows are dimmed and only the summary above them warns.
func logStatusTone(cell string) kit.TextStyle {
	switch cell {
	case logVerified.label():
		return kit.StyleOK
	case logUnverified.label():
		return kit.StyleHint
	case logGap.label():
		return kit.StyleWarn
	case logAltered.label():
		return kit.StyleFail
	}
	return kit.StyleText
}

// logResultOK is the result cell of a call that went through.
const logResultOK = "ok"

// logResultTone styles the result of a call: ok in the success colour, anything but ok in the failure colour.
func logResultTone(cell string) kit.TextStyle {
	if cell == logResultOK {
		return kit.StyleOK
	}
	if cell == "-" || cell == "" {
		return kit.StyleHint
	}
	return kit.StyleFail
}

// newLogList is the list of the log rows. The log is read only, so it has no group, tick or button; the
// search key starts the filter because the letters of the screen are keys. Left and right only move the
// cursor of the typed search, since there is no group to fold. The sentence of an empty list is
// the one for a filter that keeps no row.
func newLogList() *kit.List {
	l, err := kit.NewList(kit.ListSpec{Columns: logListColumns, Search: kit.SearchByKey, Empty: tLogNoMatch,
		Keys: kit.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome,
			Last: actEnd, Left: actLeft, Right: actRight, Enter: actEnter, Search: actSearch, Back: actBack}}, keymap, translator(false))
	if err != nil {
		panic("the list of the Logs is invalid: " + err.Error())
	}
	return l
}

// syncList makes the list anew where there is none or the language changed, which its titles and sentences
// are made in, and hands it the rows again.
func (lv *logView) syncList() {
	if lv.list != nil && lv.listLang == uiLang {
		return
	}
	lv.list, lv.listLang = newLogList(), uiLang
	lv.fill()
}

// fill hands the list the rows every filter keeps, in file order. The search of the list narrows them
// further. SearchText adds the effect and the status to what the cells say.
func (lv *logView) fill() {
	items := make([]kit.ListItem, 0, len(lv.rows))
	for i, row := range lv.rows {
		keep := true
		for k, want := range lv.filters {
			if want != "" && row.values[k] != want {
				keep = false
				break
			}
		}
		if keep {
			items = append(items, kit.ListItem{ID: kit.ItemID(strconv.Itoa(i)), Cells: row.cells,
				SearchText: row.values[logFilterEffect] + " " + row.status.label()})
		}
	}
	if err := lv.list.SetItems(items); err != nil {
		// Never an entry the kit refused shown as if it were a row.
		_ = lv.list.SetItems(nil)
	}
}

// shownRows are the rows the filters and the search keep, in the order the list shows them.
func (lv *logView) shownRows() []*logRow {
	ids := lv.list.Shown()
	out := make([]*logRow, 0, len(ids))
	for _, id := range ids {
		if row := lv.row(string(id)); row != nil {
			out = append(out, row)
		}
	}
	return out
}

// shownCount is how many rows the filters and the search keep.
func (lv *logView) shownCount() int { return len(lv.list.Shown()) }

// markedRow is the index in lv.rows of the marked row.
func (lv *logView) markedRow() (int, bool) {
	id, ok := lv.list.Selected()
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(string(id))
	return i, err == nil && i >= 0 && i < len(lv.rows)
}
