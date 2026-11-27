package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A value of the log with a control character or a terminal sequence reaches neither a cell, the search
// text, nor the ID of a row, and the drawn row shows no sequence.
func TestLogRowsHoldNoControlCharacterFromTheLog(t *testing.T) {
	m := sizedModel(groupModel(t), 140, 40)
	entry := invokelog.Entry{Path: "mcp\x00", Operation: "tool\x07bell", Connection: "conn\x1b[2Jclear",
		Effect: "read\x1b]0;evil\x07", Result: "fail\r\nforged", Client: &invokelog.ClientInfo{Name: "cl\x1b]52;c;ZXZpbA==\x07", Version: "1"}}
	row := m.logRowOf("2026-01-01", invokelog.LineResult{Parsed: true, Entry: entry}, false)
	lv := m.logs
	lv.rows, lv.loaded = []logRow{row}, true
	lv.applyFilters()
	item, ok := lv.list.Current()
	if !ok {
		t.Fatal("the row is not listed")
	}
	if _, err := strconv.Atoi(string(item.ID)); err != nil {
		t.Errorf("the ID %q is not the index of the row", item.ID)
	}
	for _, text := range append(append([]string{}, item.Cells...), item.SearchText, string(item.ID)) {
		if strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
			t.Errorf("%q holds a control character", text)
		}
	}
	m.screen, m.section = screenLogs, sectionLogs
	view := m.render()
	for _, raw := range []string{"\x07", "\x1b]", "\x1b[2J", "forged\r"} {
		if strings.Contains(view, raw) {
			t.Errorf("the screen holds %q from the log", raw)
		}
	}
}

// BenchmarkLogRowsView draws the rows of a log of 100,000 lines in the columns of the Logs, 150 by 40 cells.
func BenchmarkLogRowsView(b *testing.B) {
	lv := &logView{}
	lv.rows = make([]logRow, 100000)
	for i := range lv.rows {
		cells := []string{logVerified.label(), fmt.Sprintf("2026-02-%02d 08:%02d+00", i%28+1, i%60), "MCP", "claude-code",
			fmt.Sprintf("wiki.pages.operation-%d", i%50), "cloud-files", "ok", "120 tok", fmt.Sprintf("%dms", i%900)}
		lv.rows[i] = logRow{cells: cells, values: [logFilterCount]string{"MCP", "claude-code", "t", "c", "read", "ok"}}
	}
	lv.syncList()
	lv.fill()
	b.ResetTimer()
	for b.Loop() {
		lv.list.View(styles.Styles, 150, 40)
	}
}

// The footer names the marked row by the list's position, with the count of all rows once a search narrows
// them, and the selection stays on its row when the filters change.
func TestLogRowsPositionAndSelectionFollowTheList(t *testing.T) {
	f := newLogFixture(t)
	for _, result := range []string{"success", "denied", "success", "success", "denied"} {
		f.append("mcp", "claude-code", "wiki.pages.list", "wiki", result)
	}
	m := sizedModel(f.model(false, f.localToday()), 140, 40)
	total := len(m.logs.rows)
	if got, want := m.logPosition(), fmt.Sprintf("1/%d", total); got != want {
		t.Fatalf("position %q, want %q", got, want)
	}
	press(t, m, "/")
	typeText(t, m, "denied")
	press(t, m, "enter")
	if got, want := m.logPosition(), fmt.Sprintf("1/2 (%d total)", total); got != want {
		t.Errorf("position with a search %q, want %q", got, want)
	}
	press(t, m, "esc")
	press(t, m, "down", "down")
	marked, _ := m.logs.list.Selected()
	m.logs.filters[logFilterResult] = logResultOK
	m.logs.applyFilters()
	if id, _ := m.logs.list.Selected(); id != marked {
		t.Errorf("the mark moved from row %q to %q while it is still shown", marked, id)
	}
}

// callsOf adds the calls column of the lines of a summary list.
func callsOf(t *testing.T, items []kittui.ListItem) int {
	t.Helper()
	sum := 0
	for _, item := range items {
		n, err := strconv.Atoi(item.Cells[0])
		if err != nil {
			t.Fatalf("calls %q: %v", item.Cells[0], err)
		}
		sum += n
	}
	return sum
}

// The counts of the summary are those of the rows shown: a search and a filter narrow them, and the
// sessions list takes the same rows.
func TestLogSummaryFollowsTheShownRows(t *testing.T) {
	f := newLogFixture(t)
	for _, result := range []string{"success", "denied", "success", "success", "denied"} {
		f.append("mcp", "claude-code", "wiki.pages.list", "wiki", result)
	}
	m := sizedModel(f.model(false, f.localToday()), 140, 40)
	total := len(m.logs.rows)
	if got := callsOf(t, m.logStatItems()); got != total {
		t.Fatalf("the summary counts %d calls of %d rows", got, total)
	}
	press(t, m, "/")
	typeText(t, m, "denied")
	press(t, m, "enter")
	narrow := m.logs.shownCount()
	if got := callsOf(t, m.logStatItems()); got != narrow || narrow >= total {
		t.Errorf("with a search the summary counts %d calls of %d shown rows (of %d)", got, narrow, total)
	}
	press(t, m, "esc")
	m.logs.filters[logFilterResult] = logResultOK
	m.logs.applyFilters()
	if got := callsOf(t, m.logStatItems()); got != m.logs.shownCount() {
		t.Errorf("with a filter the summary counts %d calls of %d shown rows", got, m.logs.shownCount())
	}
}

// A value of the log with a control character or a terminal sequence reaches neither a cell of the summary
// nor of the sessions, nor the ID of a line, and the drawn screen shows no sequence.
func TestLogSummaryHoldsNoControlCharacterFromTheLog(t *testing.T) {
	m := sizedModel(groupModel(t), 140, 40)
	entry := invokelog.Entry{Path: "mcp", Session: "ab\x1b]0;evil\x07cd", Operation: "tool\x07bell\x1b[2J",
		Connection: "conn\x1b]52;c;ZXZpbA==\x07", Effect: "read", Result: "ok"}
	lv := m.logs
	lv.rows, lv.loaded = []logRow{m.logRowOf("2026-01-01", invokelog.LineResult{Parsed: true, Entry: entry}, false)}, true
	lv.applyFilters()
	m.screen, m.section = screenLogs, sectionLogs
	for _, sessions := range []bool{false, true} {
		lv.stats, lv.sessions = true, sessions
		items := m.logStatItems()
		if sessions {
			items = m.logSessionItems()
		}
		if len(items) == 0 {
			t.Fatalf("sessions %v: no line", sessions)
		}
		for _, item := range items {
			if _, err := strconv.Atoi(string(item.ID)); err != nil {
				t.Errorf("the ID %q is not an index", item.ID)
			}
			for _, cell := range item.Cells {
				if strings.IndexFunc(cell, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
					t.Errorf("sessions %v: %q holds a control character", sessions, cell)
				}
			}
		}
		view := m.render()
		for _, raw := range []string{"\x07", "\x1b]", "\x1b[2J"} {
			if strings.Contains(view, raw) {
				t.Errorf("sessions %v: the screen holds %q from the log", sessions, raw)
			}
		}
	}
}
