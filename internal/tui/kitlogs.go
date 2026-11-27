package tui

import (
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
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

// logStatColumns are the columns of the summary s shows. The count leads and is never cut; the sizes are
// token estimates (invokelog.EstimateTokens), and the bytes and the missing counters go in a narrow terminal.
var logStatColumns = []kit.Column{
	{Title: columnID("calls"), Right: true}, {Title: columnID("tool"), Flex: true},
	{Title: columnID("connection"), Optional: true}, {Title: columnID("~tok"), Right: true},
	{Title: columnID("max"), Right: true}, {Title: columnID("p50"), Optional: true, Right: true},
	{Title: columnID("p95"), Right: true}, {Title: columnID("bytes"), Optional: true, Right: true},
	{Title: columnID("no size"), Optional: true, Right: true},
}

// logSessionColumns are the columns of the MCP session summary. The first column holds the session, its
// areas, and their tools as indented text; the byte split of a request and a response goes in a narrow
// terminal before the total and the token estimate do.
var logSessionColumns = []kit.Column{
	{Title: columnID("session / area / tool")}, {Title: columnID("calls"), Right: true},
	{Title: columnID("req B"), Optional: true, Right: true}, {Title: columnID("resp B"), Optional: true, Right: true},
	{Title: columnID("total B"), Right: true}, {Title: columnID("~tokens"), Right: true},
}

// newSummaryList is a list of the summary: a short read-only table that only moves its mark, with no search
// (the search of the rows narrows what it counts), and empty is its sentence for no line. Enter and back are
// named for the kit, as left and right are, but never reach the list: the screen handles them.
func newSummaryList(columns []kit.Column, empty textID) *kit.List {
	l, err := kit.NewList(kit.ListSpec{Columns: columns, Search: kit.SearchNone, Empty: empty,
		Keys: kit.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome,
			Last: actEnd, Left: actLeft, Right: actRight, Enter: actEnter, Back: actBack}}, keymap, translator(false))
	if err != nil {
		panic("the summary of the Logs is invalid: " + err.Error())
	}
	return l
}

// summaryList is the list of the summary the tab shows, made anew where there is none or the language changed
// and filled with the rows the filters and the search keep. A line stands for its index, never for a value of
// the log.
func (m *Model) summaryList() *kit.List {
	lv := m.logs
	if lv.statList == nil || lv.summaryLang != uiLang {
		lv.statList, lv.sessionList, lv.summaryLang = newSummaryList(logStatColumns, tLogNoMatch),
			newSummaryList(logSessionColumns, tLogNoSession), uiLang
	}
	l, items := lv.statList, m.logStatItems()
	if lv.sessions {
		l, items = lv.sessionList, m.logSessionItems()
	}
	if err := l.SetItems(items); err != nil {
		// Never an entry the kit refused shown as if it were a line.
		_ = l.SetItems(nil)
	}
	return l
}

// logStatItems are the groups of invokelog.Summarize over the shown rows, a line each.
func (m *Model) logStatItems() []kit.ListItem {
	groups := invokelog.Summarize(m.logs.shownEntries())
	num := func(n int64) string { return strconv.FormatInt(n, 10) }
	items := make([]kit.ListItem, 0, len(groups))
	for i, g := range groups {
		cells := []string{strconv.Itoa(g.Calls), m.logText(g.Operation), orDash(m.logText(g.Connection)),
			num(invokelog.EstimateTokens(g.Bytes)), num(invokelog.EstimateTokens(g.Max)),
			num(invokelog.EstimateTokens(g.P50)), num(invokelog.EstimateTokens(g.P95)), num(g.Bytes),
			strconv.Itoa(g.NoCounter)}
		if g.Counted() == 0 {
			// No invoke of the group has a recorded size; that is not a size of 0.
			for k := 3; k <= 7; k++ {
				cells[k] = "-"
			}
		}
		items = append(items, kit.ListItem{ID: kit.ItemID(strconv.Itoa(i)), Cells: cells})
	}
	return items
}

// logSessionItems are a line for each MCP session of the shown rows, its areas, and their tools, flat, with
// the level shown as indent in the first cell.
func (m *Model) logSessionItems() []kit.ListItem {
	var items []kit.ListItem
	add := func(label string, t invokelog.ToolBytes) {
		items = append(items, kit.ListItem{ID: kit.ItemID(strconv.Itoa(len(items))), Cells: []string{label,
			strconv.Itoa(t.Calls), strconv.FormatInt(t.ReqBytes, 10), strconv.FormatInt(t.RespBytes, 10),
			strconv.FormatInt(t.Total(), 10), strconv.FormatInt(invokelog.EstimateTokens(t.Total()), 10)}})
	}
	for _, session := range invokelog.SummarizeSessions(m.logs.shownEntries()) {
		var sum invokelog.ToolBytes
		for _, a := range session.Areas {
			sum.Calls += a.Calls
			sum.ReqBytes += a.ReqBytes
			sum.RespBytes += a.RespBytes
		}
		add(tr(tLogSessionRow, m.logText(session.Session)), sum)
		for _, a := range session.Areas {
			add("  "+m.logText(string(a.Area)), a.ToolBytes)
			for _, t := range a.Tools {
				add("    "+orDash(m.logText(t.Operation)), t)
			}
		}
	}
	return items
}
