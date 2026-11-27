// The Logs section (the Activity area, see the sectionLogs comment in tui.go): the invocation log, read only. A bar on
// top (logbar.go) chooses a day, a range of days, or every recorded day and narrows the rows by way, MCP client,
// tool, connection, effect, and result; / searches the text of the rows; the tabs of the bar show the
// rows (Table), a heatmap of their days (Activity, activity.go), or the response sizes summed up per tool and
// connection (Details; s toggles it, m shows the context cost of each MCP session instead); enter opens one
// entry. Every row and the bar carry the status the hash chain and the check values give it, found by the same walk
// 'qconnect vault logs verify' makes (invokelog.VerifyLines) with the same checker. Reading and checking run as
// a command, never on the event loop, and never ask for a passphrase: a locked vault leaves the check values
// unchecked, which shows as unverified.
package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-connect/internal/vaultproc"
	"github.com/castrowithcee/qatlas-kit/i18n"
	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// logDateLayout is how a local day is typed and shown.
const logDateLayout = "2006-01-02"

// logMode is what the bar's Mode field selects: one day, a range of days, or every recorded day.
type logMode int

const (
	logModeDay logMode = iota
	logModeRange
	logModeAll
	logModeCount
)

func (mode logMode) String() string {
	return tr([...]textID{tLogModeDay, tLogModeRange, tLogModeAll}[mode])
}

// logStatus is what the chain and the check values say about one line, ordered from best to worst so the
// bar can show the worst of what it selects.
type logStatus int

const (
	logVerified logStatus = iota
	logUnverified
	logGap
	logAltered
)

// label is the status as the screen shows it: always symbol and word together, so it reads without colour.
func (s logStatus) label() string {
	return tr([...]textID{tLogVerified, tLogUnverified, tLogGap, tLogAltered}[s])
}

func (s logStatus) style() lipgloss.Style {
	return [...]lipgloss.Style{styles.ok, styles.warning, styles.warning, styles.fail}[s]
}

// logFocus is the part of the Logs screen the keys act on: the rows, the tabs, or the filter bar.
type logFocus int

const (
	logFocusRows logFocus = iota
	logFocusTabs
	logFocusBar
)

// logTab is what the tabs of the Logs screen select: the rows, the heatmap of their days, or the summary.
type logTab int

const (
	logTabTable logTab = iota
	logTabActivity
	logTabDetails
	logTabCount
)

// logTabWords are the names of the tabs in the language of the editor.
func logTabWords() []string {
	return []string{tr(tLogTabTable), tr(tLogTabActivity), tr(tLogTabDetails)}
}

// tab is the tab the view shows.
func (lv *logView) tab() logTab {
	switch {
	case lv.heat:
		return logTabActivity
	case lv.stats:
		return logTabDetails
	}
	return logTabTable
}

// setLogTab shows a tab: the rows, the heatmap, or the summary of the rows (the MCP sessions of it follow m).
func (m *Model) setLogTab(tab logTab) tea.Cmd {
	lv := m.logs
	switch tab {
	case logTabActivity:
		return m.setHeat(true)
	case logTabDetails:
		lv.stats, lv.sessions, lv.heat, lv.scroll = true, false, false, 0
	default:
		lv.stats, lv.sessions, lv.heat = false, false, false
	}
	return nil
}

// The filters of the bar, in its order.
const (
	logFilterWay = iota
	logFilterClient
	logFilterTool
	logFilterConnection
	logFilterEffect
	logFilterResult
	logFilterCount
)

var (
	logFilterShort = [logFilterCount]textID{tLogFilterShortWay, tLogFilterShortClient, tLogFilterShortTool,
		tLogFilterShortConn, tLogFilterShortEffect, tLogFilterShortResult}
)

var logLabels = map[string]textID{
	"status":         tLogLabelStatus,
	"UTC file":       tLogLabelUTCFile,
	"sequence":       tLogLabelSequence,
	"day":            tLogLabelDay,
	"kind":           tLogLabelKind,
	"through":        tLogLabelThrough,
	"way":            tLogLabelWay,
	"client":         tLogLabelClient,
	"tool":           tLogLabelTool,
	"requested tool": tLogLabelRequestedTool,
	"effect":         tLogLabelEffect,
	"result":         tLogLabelResult,
	"duration":       tLogLabelDuration,
	"request":        tLogLabelRequest,
	"response":       tLogLabelResponse,
}

// logLabel is a row label of the entry detail in the editor's language.
func logLabel(english string) string {
	if id, ok := logLabels[english]; ok {
		return tr(id)
	}
	return labelText(english)
}

// logColumnTitles draws the column titles of a table of the Logs section in the editor's language, in capitals.
func logColumnTitles(columns []column) []column {
	out := make([]column, len(columns))
	for i, c := range columns {
		c.title = strings.ToUpper(columnText(c.title))
		out[i] = c
	}
	return out
}

// logStatColumns are the columns of the summary s shows. The count leads and is never cut; the sizes are
// token estimates (invokelog.EstimateTokens), and the bytes and the missing counters go in a narrow terminal.
var logStatColumns = []column{
	{title: "calls"}, {title: "tool", flex: true}, {title: "connection", optional: true},
	{title: "~tok"}, {title: "max"}, {title: "p50", optional: true}, {title: "p95"},
	{title: "bytes", optional: true}, {title: "no size", optional: true},
}

// logSessionColumns are the columns of the MCP session summary. The first column holds the session, its
// areas, and their tools as indented text; the byte split of a request and a response goes in a narrow
// terminal before the total and the token estimate do.
var logSessionColumns = []column{
	{title: "session / area / tool"}, {title: "calls"}, {title: "req B", optional: true},
	{title: "resp B", optional: true}, {title: "total B"}, {title: "~tokens"},
}

// maxLogText bounds one field of a log line on screen; a well-formed entry never comes close.
const maxLogText = 256

// logRow is one line of a day file as the table shows it. cells and values are already made safe to print.
type logRow struct {
	date   string
	line   invokelog.LineResult
	status logStatus
	cells  []string
	values [logFilterCount]string
}

// logView is the state of the Logs section.
type logView struct {
	// view scrolls the detail screen of one entry; page is how many of its lines the last drawing showed.
	view textScroll
	page int
	// position is the place of the marked row among the shown ones, as the full frame of the table names it.
	position string
	mode     logMode
	// day is the day of logModeDay, from and to the range of logModeRange, all as logDateLayout.
	day, from, to string
	focus         logFocus
	// zoneTabs and zoneBar are the lines of the tab line and of the filter bar with the search under it in the
	// head of the last frame, counted from its first line.
	zoneTabs, zoneBar [2]int

	// loadID guards against an answer to a load that a later one replaced; loading is true until the latest
	// one answered, and loaded once any did.
	loadID  int
	loading bool
	loaded  bool
	// span is the from and to the rows were loaded for.
	span [2]string
	// err is why the log could not be read at all; note why check values went unchecked.
	err  string
	note string

	rows  []logRow
	worst logStatus
	// filters holds the value each filter keeps, "" for all.
	filters [logFilterCount]string
	// list shows the rows the filters keep, by their index in rows, and holds the search over them; it is made
	// anew when the language changed from listLang.
	list     *kit.List
	listLang i18n.Lang

	// stats shows the summary of the shown rows instead of the rows themselves, heat the heatmap of their days;
	// the tab says which (see tab).
	stats bool
	heat  bool
	// framed says that the last frame drew the rows in a panel.
	framed bool
	// compact says that the last frame left out the panel and the explanations, which the info key gives.
	compact bool
	// sessions makes the summary the one of the MCP sessions, scrolled down by scroll lines.
	sessions bool
	scroll   int
	// bar is the filter bar of the mode, the date and the filters; barTargets are its targets in the last
	// frame, counted from the left of its line.
	bar        *kit.FilterBar
	barTargets []kit.Target
	// detail is the index of the row the detail screen shows.
	detail int
}

func newLogView(now time.Time) *logView {
	today := now.In(time.Local)
	lv := &logView{
		day:  today.Format(logDateLayout),
		from: today.AddDate(0, 0, -6).Format(logDateLayout),
		to:   today.Format(logDateLayout),
	}
	lv.syncList()
	return lv
}

func (lv *logView) row(name string) *logRow {
	i, err := strconv.Atoi(name)
	if err != nil || i < 0 || i >= len(lv.rows) {
		return nil
	}
	return &lv.rows[i]
}

// dateText is the text of the date field of the bar: the day, or from and to.
func (lv *logView) dateText() string {
	if lv.mode == logModeRange {
		return lv.from + ".." + lv.to
	}
	return lv.day
}

// setFocus moves the focus to f, into the bar at its first field or, fromEnd, its last.
func (lv *logView) setFocus(f logFocus, fromEnd bool) {
	if lv.bar != nil {
		switch {
		case f != logFocusBar:
			lv.bar.Blur()
		case lv.focus != logFocusBar || !lv.bar.Focused():
			lv.bar.Focus(fromEnd)
		}
	}
	lv.focus = f
}

// bounds is the from and to of the selected days; "" leaves an end open.
func (lv *logView) bounds() (string, string) {
	switch lv.mode {
	case logModeDay:
		return lv.day, lv.day
	case logModeRange:
		return lv.from, lv.to
	}
	return "", ""
}

// logsLoadedMsg carries the lines of the selected days and their status back into the event loop.
type logsLoadedMsg struct {
	id   int
	from string
	to   string
	days []invokelog.DayLines
	note string
	err  error
}

// logVault is the vault whose logs directory the section reads: the run's own, or, for a run without one,
// the vault beside the configuration, the same one 'qconnect vault logs verify' reads.
func (m *Model) logVault() *vault.Vault {
	if v := m.secrets.Vault(); v != nil {
		return v
	}
	return vault.New(filepath.Dir(m.svc.Path()))
}

// refreshLogRetention reads the retention of the vault's log from its effective settings (settings.age,
// never the configuration file) and keeps it for drawing. An untrusted settings.age reads as never
// truncating, as the writers treat it.
func (m *Model) refreshLogRetention() {
	settings, _ := vault.EffectiveSettings(m.logVault())
	m.logRetention = settings.RetentionDays
}

// loadLogs reads and checks the UTC files overlapping the selected local days as a command. The walk
// reads every file for the chain, but keeps and checks only the overlapping files.
func (m *Model) loadLogs() tea.Cmd {
	lv := m.logs
	lv.loadID++
	lv.loading = true
	id := lv.loadID
	from, to := lv.bounds()
	if lv.bar != nil {
		// The bar of All has no date field.
		_ = lv.bar.SetActionText(logBarDate, lv.dateText())
	}
	utcFrom, utcTo := logUTCFileBounds(from, to)
	v := m.logVault()
	return func() tea.Msg {
		checker, note, done := logChecker(context.Background(), v)
		defer done()
		days, err := invokelog.VerifyLines(v.Dir(), checker, utcFrom, utcTo)
		if err != nil && checker != nil {
			// A check that cannot be made at all never passes a line as verified; the lines go unchecked.
			note = tr(tLogCheckFailed, err.Error())
			days, err = invokelog.VerifyLines(v.Dir(), nil, utcFrom, utcTo)
		}
		return logsLoadedMsg{id: id, from: from, to: to, days: days, note: note, err: err}
	}
}

// logUTCFileBounds selects every UTC day file that can overlap the chosen local days.
func logUTCFileBounds(from, to string) (string, string) { return invokelog.UTCFileBounds(from, to) }

// logChecker returns what checks the log's check values of v, the same way 'qconnect vault logs verify' does:
// the key of a vault unlocked in this process; else a running vault process, which only ever answers
// whether a check value matches; else nothing, since the editor never asks for the passphrase for this.
// note says why there is no checker, and done clears a key this derived.
func logChecker(ctx context.Context, v *vault.Vault) (invokelog.Checker, string, func()) {
	nothing := func() {}
	locked := tr(tLogLocked)
	state, err := v.State()
	switch {
	case err != nil:
		return nil, tr(tLogStateUnreadable), nothing
	case state == vault.StateUnlocked:
		key, err := v.LogKey()
		if err != nil {
			return nil, locked, nothing
		}
		return key, "", key.Clear
	case state != vault.StateLocked:
		return nil, tr(tLogNotEncrypted), nothing
	case !vaultproc.Supported:
		return nil, locked, nothing
	}
	client, err := vaultproc.ProcessClientOf(v)
	if err != nil {
		return nil, locked, nothing
	}
	if _, err := client.Status(ctx); err != nil {
		if !errors.Is(err, vaultproc.ErrNotRunning) {
			return nil, tr(tLogProcessCannot, err.Error()), nothing
		}
		return nil, locked, nothing
	}
	return client.LogChecker(ctx), "", nothing
}

// handleLogsLoaded takes the answer of the latest load; an earlier one is dropped.
func (m *Model) handleLogsLoaded(msg logsLoadedMsg) tea.Cmd {
	lv := m.logs
	if msg.id != lv.loadID {
		return nil
	}
	lv.loading, lv.loaded = false, true
	lv.err, lv.note = "", ""
	if msg.err != nil {
		lv.err = m.redactor.Apply(msg.err.Error())
	}
	if msg.note != "" {
		lv.note = m.redactor.Apply(msg.note)
	}
	lv.rows, lv.worst = nil, logVerified
	for _, day := range msg.days {
		for _, line := range day.Lines {
			// An undated line cannot be assigned to a local day. Keep it with its overlapping UTC
			// file so the selected view still shows its verification status.
			if line.Parsed && !line.Entry.Time.IsZero() &&
				((msg.from != "" && line.Entry.Time.In(time.Local).Format(logDateLayout) < msg.from) ||
					(msg.to != "" && line.Entry.Time.In(time.Local).Format(logDateLayout) > msg.to)) {
				continue
			}
			row := m.logRowOf(day.Date, line, msg.from != msg.to || msg.from == "")
			lv.worst = max(lv.worst, row.status)
			lv.rows = append(lv.rows, row)
		}
	}
	span := [2]string{msg.from, msg.to}
	if span != lv.span {
		lv.list.Reset()
	}
	lv.span = span
	lv.applyFilters()
	m.setLogBarOptions()
	return nil
}

// statusOf is the status of one line: altered when it does not parse, fails its check value, or breaks the
// chain; a gap when entries are missing right before it; verified when its check value matched; unverified
// otherwise, which is a line without a check value or one nobody could check.
func statusOf(line invokelog.LineResult) logStatus {
	switch {
	case !line.Parsed || line.MAC == invokelog.MACInvalid || line.Chain != invokelog.ChainIntact:
		return logAltered
	case line.Missing > 0:
		return logGap
	case line.MAC == invokelog.MACValid:
		return logVerified
	}
	return logUnverified
}

// logRowOf builds the row of one line. withDate puts the local day in front of the time.
func (m *Model) logRowOf(date string, line invokelog.LineResult, withDate bool) logRow {
	row := logRow{date: date, line: line, status: statusOf(line)}
	e := line.Entry
	when, way, client, tool, conn, effect, result, dur, size := tr(tLogUTCFile, date), "-", "-", "-", "-", "-", "-", "", "-"
	switch {
	case !line.Parsed:
		tool = tr(tLogUnreadable)
	case e.Kind == invokelog.KindCut:
		if !e.Time.IsZero() {
			when = logTableTime(e.Time, withDate)
		}
		tool = tr(tLogRetentionCut)
		if e.Cut != nil {
			tool = tr(tLogRetentionCutThrough, e.Cut.ThroughSeq)
		}
	default:
		if !e.Time.IsZero() {
			when = logTableTime(e.Time, withDate)
		}
		way = orDash(strings.ToUpper(m.logText(e.Path)))
		if e.Client != nil {
			client = orDash(m.logText(e.Client.Name))
		}
		tool = orDash(m.logText(e.Operation))
		conn = orDash(m.logText(e.Connection))
		effect = orDash(m.logText(e.Effect))
		result = orDash(m.logText(e.Result))
		if e.Result == "success" {
			result = "ok"
		}
		dur = formatDuration(e.DurationMS)
		if e.RespBytes != nil {
			size = estimateTokens(*e.RespBytes)
		}
	}
	row.cells = []string{row.status.label(), when, way, client, tool, conn, result, size, dur}
	row.values = [logFilterCount]string{way, client, tool, conn, effect, result}
	return row
}

func logTableTime(t time.Time, withDate bool) string {
	local := t.In(time.Local)
	_, offset := local.Zone()
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	zone := fmt.Sprintf("%s%02d", sign, offset/3600)
	if offset%3600 != 0 {
		zone += fmt.Sprintf("%02d", offset%3600/60)
	}
	if withDate {
		return local.Format(logDateLayout+" 15:04") + zone
	}
	return local.Format("15:04") + zone
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// estimateTokens approximates the tokens of a payload of n bytes at 3.5 bytes per token.
func estimateTokens(n int64) string {
	return tr(tUnitTokens, n*2/7)
}

// formatBytes shows a byte counter with its token estimate; a nil counter was not recorded.
func formatBytes(n *int64) string {
	if n == nil {
		return "-"
	}
	return fmt.Sprintf("%d B (%s)", *n, estimateTokens(*n))
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// logText makes one field of a log line safe to print. A log file is not trusted: a changed line may hold
// anything, so a registered secret is redacted, invalid UTF-8, control characters and invisible formatting
// characters (which terminal escape sequences and direction overrides are made of) are replaced, and an
// overlong value is cut.
func (m *Model) logText(s string) string {
	s = strings.ToValidUTF8(m.redactor.Apply(s), "�")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return '�'
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > maxLogText {
		s = string([]rune(s)[:maxLogText-1]) + "…"
	}
	return s
}

// applyFilters shows the rows every filter keeps, in file order; the search then narrows them further.
func (lv *logView) applyFilters() {
	lv.syncList()
	lv.fill()
}

// openLogs shows the Logs section with the rows in focus and loads the selected days anew.
func (m *Model) openLogs() tea.Cmd {
	m.stopTest()
	m.testName, m.editing, m.confirmRole = "", "", ""
	m.clearMessages()
	m.refreshLogRetention()
	lv := m.logs
	lv.setFocus(logFocusRows, false)
	lv.stats, lv.sessions, lv.heat = false, false, false
	m.syncLogBar()
	m.screen = screenLogs
	return m.loadLogs()
}

// reloadShownLogs loads the Logs section again when it is the active section, so a vault unlocked or locked
// from it shows the check values checked or unchecked at once.
func (m *Model) reloadShownLogs() tea.Cmd {
	if m.section != sectionLogs || !m.logs.loaded {
		return nil
	}
	return m.loadLogs()
}

// updateLogs handles the keys of the Logs screen and of its bar.
func (m *Model) updateLogs(key tea.KeyPressMsg) tea.Cmd {
	lv := m.logs
	if lv.list.Searching() {
		return m.updateLogSearch(key)
	}
	if lv.focus == logFocusBar {
		if cmd, handled := m.updateLogBar(key); handled {
			return cmd
		}
	}
	if s, ok := sectionShortcut(key.String()); ok {
		return m.openSection(s)
	}
	switch {
	case keyIs(key, actQuit):
		return m.quit()
	case keyIs(key, actBack):
		if lv.stats {
			lv.stats, lv.sessions = false, false
			return nil
		}
		if _, cleared := lv.list.Act(actBack); cleared {
			return nil
		}
		m.focusNav()
	case keyIs(key, actNext):
		lv.moveFocus(1)
	case keyIs(key, actPrev):
		lv.moveFocus(-1)
	case keyIs(key, actLeft) || keyIs(key, actRight):
		by := 1
		if keyIs(key, actLeft) {
			by = -1
		}
		if lv.focus == logFocusTabs {
			return m.setLogTab(logTab(min(max(int(lv.tab())+by, 0), int(logTabCount)-1)))
		}
	case keyIs(key, actSummary):
		lv.stats, lv.sessions, lv.heat = !lv.stats, false, false
	case keyIs(key, actSessions):
		on := !(lv.stats && lv.sessions)
		lv.stats, lv.sessions, lv.scroll, lv.heat = on, on, 0, false
	case keyIs(key, actEnter):
		switch lv.focus {
		case logFocusTabs:
			lv.setFocus(logFocusRows, false)
		case logFocusRows:
			if i, ok := lv.markedRow(); ok && !lv.stats && !lv.heat {
				lv.detail = i
				lv.view.reset()
				m.screen = screenLogDetail
				m.clearMessages()
			}
		}
	case keyIs(key, actUp) || keyIs(key, actDown) || keyIs(key, actPageUp) || keyIs(key, actPageDown) || keyIs(key, actHome) || keyIs(key, actEnd):
		lv.setFocus(logFocusRows, false)
		if lv.heat {
			break
		}
		if lv.stats && lv.sessions {
			m.scrollSessions(key)
			break
		}
		lv.list.Update(key)
	case keyIs(key, actFilter):
		if lv.focus != logFocusBar {
			m.logBar()
			lv.setFocus(logFocusBar, false)
		}
	case keyIs(key, actSearch):
		lv.setFocus(logFocusRows, false)
		lv.list.Act(actSearch)
	}
	return nil
}

// moveFocus steps from the tabs to the bar and the rows and round; the bar steps through its own fields first.
func (lv *logView) moveFocus(by int) {
	order := []logFocus{logFocusTabs, logFocusBar, logFocusRows}
	at := len(order) - 1
	for i, f := range order {
		if f == lv.focus {
			at = i
		}
	}
	lv.setFocus(order[wrap(at+by, len(order))], by < 0)
}

// updateLogSearch is the Logs screen while its search is typed; every printable key is search text.
func (m *Model) updateLogSearch(key tea.KeyPressMsg) tea.Cmd {
	if content, ok := pasted(key); ok {
		m.logs.list.Paste(content)
		return nil
	}
	m.logs.list.Update(key)
	return nil
}

// updateLogDetail handles the detail screen of one entry.
func (m *Model) updateLogDetail(key tea.KeyPressMsg) tea.Cmd {
	if s, ok := sectionShortcut(key.String()); ok {
		return m.openSection(s)
	}
	switch {
	case keyIs(key, actBack):
		m.screen = screenLogs
	default:
		m.logs.view.scroll(key, m.logs.page)
	}
	return nil
}

// logsView draws the Logs section: the tabs, the filter and search lines, the panel with the table, the heatmap
// or the summary, and the keys. It is also what the workspace shows below the tab line while the section is only
// marked there.
func (m *Model) logsView() string {
	lv := m.logs
	head, foot := m.logsFrame()
	var b strings.Builder
	b.WriteString(head)
	m.hitBar(strings.Split(head, "\n"), m.workspaceTop(), m.logBarSpecs)
	m.addLogBarTargets(lv.barTargets, 0)
	room := m.height - strings.Count(head, "\n") - strings.Count(foot, "\n") - 1
	if view, targets := m.logBar().ValueList(styles.Styles, m.logRoom(), room); view != "" {
		// The open values of a field take the place of the rows below the bar.
		b.WriteString(m.logInside(view + "\n"))
		m.addLogBarTargets(targets, strings.Count(head, "\n")-lv.zoneBar[0])
		m.zoneLogs(b.String())
		b.WriteString(foot)
		return b.String()
	}
	if lv.stats && lv.loaded && lv.err == "" && lv.shownCount() > 0 {
		if lv.sessions {
			b.WriteString(m.logInside(m.sessionStatLines(m.logSessionStats(), room)))
		} else {
			b.WriteString(m.logInside(m.logStatLines(m.logStats(), room)))
		}
		m.zoneLogs(b.String())
		b.WriteString(foot)
		return b.String()
	}
	if lv.heat && lv.loaded && lv.err == "" && lv.shownCount() > 0 {
		b.WriteString(m.logHeatLines(room))
		m.zoneLogs(b.String())
		b.WriteString(foot)
		return b.String()
	}
	if lv.tab() == logTabTable && lv.loaded && lv.err == "" && len(lv.rows) > 0 {
		lv.syncList()
		lv.list.SetFocused(m.screen == screenLogs && lv.focus == logFocusRows)
		view, targets := lv.list.View(styles.Styles, m.logRoom(), room)
		lines := strings.Split(view, "\n")
		// The list fills its height with blank lines; the panel closes below the last one it drew.
		for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
			lines = lines[:len(lines)-1]
		}
		for _, line := range lines {
			b.WriteString(m.logSide(line) + "\n")
		}
		m.addLogRowTargets(targets, head, len(lines))
	}
	m.zoneLogs(b.String())
	b.WriteString(foot)
	return b.String()
}

// zoneLogs registers the zones of the Logs screen, whose lines up to its foot are drawn: the panel with its
// table, summary or heatmap takes the rows and the tab line the tabs; the bar has a free area of its own. The
// frame of the panel, where there is one, belongs to the panel.
func (m *Model) zoneLogs(drawn string) {
	lv := m.logs
	top, width := m.workspaceTop(), m.usable(0)
	end := top + strings.Count(drawn, "\n")
	if lv.framed {
		end++
	}
	m.addZone(0, top, width, end, func() tea.Cmd { return m.zoneLogFocus(logFocusRows) })
	m.addZone(0, top+lv.zoneTabs[0], width, top+lv.zoneTabs[1], func() tea.Cmd { return m.zoneLogFocus(logFocusTabs) })
}

// addLogBarTargets places targets of the bar, counted from the left of the bar's line and from dy lines below
// it, on the screen: inside the frame of the panel where there is one.
func (m *Model) addLogBarTargets(targets []kit.Target, dy int) {
	if m.backdrop || m.tooSmall {
		return
	}
	x := 0
	if m.logs.framed {
		x = 2
	}
	m.barTargets = append(m.barTargets, kit.OffsetTargets(targets, x, m.workspaceTop()+m.logs.zoneBar[0]+dy)...)
}

// zoneLogFocus is the click on the free area of a part of the Logs screen: that part takes the focus, with the
// checks the other clicks of the screen meet.
func (m *Model) zoneLogFocus(focus logFocus) tea.Cmd {
	lead, ok := m.logBarGuard()
	if ok {
		m.logs.setFocus(focus, false)
	}
	return lead
}

// logRoom is the width of the lines inside the panel of the last frame, or of the whole workspace where the
// frame drew none.
func (m *Model) logRoom() int {
	if m.logs.framed {
		return max(m.usable(0)-4, 1)
	}
	return m.usable(0)
}

// logPanelFocused reports whether the frame of the panel shows the focus: the screen has it, not the area tabs.
func (m *Model) logPanelFocused() bool {
	return m.screen == screenLogs
}

// logSide puts one line into the frame of the panel, where the last frame drew one.
func (m *Model) logSide(line string) string {
	if !m.logs.framed {
		return line
	}
	return panelSide(line, m.logRoom(), m.logPanelFocused())
}

// logInside puts every line of text, each ended by a line break, into the frame of the panel.
func (m *Model) logInside(text string) string {
	if !m.logs.framed || text == "" {
		return text
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = m.logSide(line)
	}
	return strings.Join(lines, "\n") + "\n"
}

// addLogRowTargets places the targets the list of the rows drew, from the first line below head, on the
// screen: inside the frame of the panel where there is one, and cut to the lines the list drew.
func (m *Model) addLogRowTargets(targets []kit.Target, head string, rows int) {
	if m.backdrop || m.tooSmall {
		return
	}
	x, top := 0, m.hitTop(head)
	if m.logs.framed {
		x = 2
	}
	for _, t := range kit.OffsetTargets(targets, x, top) {
		if t.Rect.Y >= top+rows {
			continue
		}
		t.Rect.Height = min(t.Rect.Height, top+rows-t.Rect.Y)
		m.listTargets = append(m.listTargets, t)
	}
}

// logSearchLine is the search line of the list for a tab that draws no rows, in width cells, or "" while the
// search is neither typed nor kept. The list draws that line first in any room of two lines.
func (lv *logView) logSearchLine(width int) string {
	if !lv.list.Searching() && lv.list.Query() == "" {
		return ""
	}
	view, _ := lv.list.View(styles.Styles, width, 2)
	line, _, _ := strings.Cut(view, "\n")
	return line
}

// logsFrame is everything of the Logs screen except its rows. The tab line carries the status of the chain;
// the bar holds the mode, the date and the filters, and the search stands under it while it is used. The rows
// stand in a panel where there is room; a terminal too low for them drops the panel and the rules first.
func (m *Model) logsFrame() (string, string) {
	lv := m.logs
	focused := m.screen == screenLogs
	lv.position = ""
	width := m.usable(0)
	status := styles.hint.Render(tr(tLogLoading))
	if lv.loaded {
		status = lv.worst.style().Render(lv.worst.label())
	}
	// bar draws the tab line, the filter bar and, while it is typed or holds a text, the search under the bar,
	// for a room of width cells: inside the panel where there is one.
	bar := func(width int) (tabLine, filterRows, compactRows []string) {
		strip, spans := tabStrip(logTabWords(), int(lv.tab()), focused && lv.focus == logFocusTabs)
		tabsPlain := make([]string, len(spans))
		for i, span := range spans {
			tabsPlain[i] = ansi.Cut(ansi.Strip(strip), span[0], span[1])
		}
		tabItem := barMarker(focused && lv.focus == logFocusTabs) + strip
		if !lv.compactSubject().empty() {
			// The compact frame leaves the explanation of the summary to the info key.
			tabItem += styles.hint.Render(infoMark)
		}
		tabLine = packBar([]string{tabItem}, status, width)
		b := m.logBar()
		if !focused && lv.focus == logFocusBar {
			// The tab line took the focus: the bar gives it up, and the rows have it on the way back.
			lv.setFocus(logFocusRows, false)
		}
		var line string
		line, lv.barTargets = b.View(styles.Styles, width)
		filterRows, compactRows = []string{line}, []string{line}
		var search string
		if lv.list.Searching() || lv.list.Query() != "" {
			lv.barTargets = nil
			if lv.tab() == logTabTable {
				// The list draws its search line above its rows, and a compact frame leaves the bar to it.
				compactRows = nil
			} else {
				search = lv.logSearchLine(width)
				filterRows = append(filterRows, search)
				// A compact frame keeps one line: the search while it is typed.
				compactRows = []string{search}
			}
		}
		m.logBarSpecs = m.logTabTargets(tabsPlain, ansi.Strip(search))
		return tabLine, filterRows, compactRows
	}
	rule := styles.quietBorder.Render(strings.Repeat("─", width))

	shown := lv.shownCount()
	// body is the lines between the top and the bottom of the panel, drawn for the room of its inside.
	body := func(room int, framed, compact bool) []string {
		var lines []string
		note := func(style lipgloss.Style, text string) {
			if framed {
				lines = append(lines, strings.Split(render(style, room, text), "\n")...)
				return
			}
			lines = append(lines, m.wrapped(style, text))
		}
		header := func(t table, l layout) string {
			return styles.hint.Render(clipCells("  "+t.header(l), room))
		}
		switch {
		case lv.err != "":
			note(styles.fail, tr(tLogError, lv.err))
		case !lv.loaded:
			note(styles.hint, tr(tLogReading))
		case len(lv.rows) == 0:
			note(lipgloss.NewStyle(), lv.emptyText())
		case shown == 0 && lv.tab() != logTabTable:
			// The list of the rows says it itself.
			note(styles.hint, tr(tLogNoMatch))
		case lv.heat:
			// The heatmap has no table header.
		case lv.stats && lv.sessions:
			if !compact {
				note(styles.hint, tr(tLogSessionsHelp))
			}
			if stats := m.logSessionStats(); len(stats.order) > 0 {
				lines = append(lines, header(stats.table, stats.layout))
			}
		case lv.stats:
			stats := m.logStats()
			if !compact {
				note(styles.hint, tr(tLogStatsHelp))
			}
			lines = append(lines, header(stats.table, stats.layout))
		}
		if framed {
			for i, line := range lines {
				lines[i] = panelSide(line, room, focused)
			}
		}
		return lines
	}

	keys := m.keyHint(m.logKeys())

	// Every part ends its line itself, so the rows stand right above the foot; a compact frame leaves out
	// the panel, the rules, the idle search line, the position, and the note, which the detail repeats.
	build := func(compact bool) (string, string) {
		framed := !compact && m.tableFramed()
		lv.framed, lv.compact = framed, compact
		room := width
		if framed {
			room = max(width-4, 1)
		}
		// Without the frame the terminal is small, and the title line would cost a row of the table.
		var lines []string
		if framed {
			lines = strings.Split(strings.TrimSuffix(m.titled(m.section.title(), "", width), "\n"), "\n")
		}
		tabLine, filterRows, compactRows := bar(room)
		bottom := rule
		if framed {
			// The bars stand inside the panel, so the frame shows the focus wherever on the screen it is.
			lines = append(lines, panelTop("", "", room, focused))
			bottom = panelBottom(room, focused)
		}
		barLines := append([]string{}, tabLine...)
		if compact {
			barLines = append(barLines, compactRows...)
		} else {
			barLines = append(barLines, filterRows...)
		}
		lv.zoneTabs = [2]int{len(lines), len(lines) + len(tabLine)}
		lv.zoneBar = [2]int{len(lines) + len(tabLine), len(lines) + len(barLines)}
		for _, line := range barLines {
			if framed {
				line = panelSide(line, room, focused)
			}
			lines = append(lines, line)
		}
		if !framed && !compact {
			lines = append(lines, rule)
		}
		lines = append(lines, body(room, framed, compact)...)
		if compact {
			return strings.Join(lines, "\n") + "\n", strings.TrimPrefix(keys, "\n") + m.notes()
		}
		foot := keys
		if shown > 0 && focused && !lv.stats && !lv.heat {
			lv.position = lv.list.Position()
		}
		if lv.note != "" && lv.loaded && lv.hasUnchecked() {
			foot = "\n" + m.wrapped(styles.hint, lv.note) + foot
		}
		return strings.Join(lines, "\n") + "\n", bottom + foot + m.notes()
	}
	head, f := build(false)
	// The full frame must leave a row for the rows or the note, whether or not there are rows to show.
	if m.height-strings.Count(head, "\n")-strings.Count(f, "\n")-1 < 1 {
		return build(true)
	}
	return head, f
}

// logPosition is the position the table of the Logs screen puts at the right end of the footer, known before
// it is drawn.
func (m *Model) logPosition() string {
	lv := m.logs
	if m.screen != screenLogs {
		return ""
	}
	m.logsFrame()
	return lv.position
}

// compactSubject is the explanation the compact frame left out of the summary tab, or none: the full frame
// shows it only above a table, so it exists only where there are rows.
func (lv *logView) compactSubject() infoSubject {
	if !lv.compact || !lv.stats || lv.heat || !lv.loaded || lv.err != "" || lv.shownCount() == 0 {
		return infoSubject{}
	}
	help := tLogStatsHelp
	if lv.sessions {
		help = tLogSessionsHelp
	}
	return infoSubject{title: logTabWords()[logTabDetails], long: tr(help)}
}

// emptyText says that the selected days hold no entry.
func (lv *logView) emptyText() string {
	switch lv.mode {
	case logModeDay:
		return tr(tLogEmptyDay, lv.day)
	case logModeRange:
		return tr(tLogEmptyRange, lv.from, lv.to)
	}
	return tr(tLogEmptyAll)
}

// hasUnchecked reports whether a row carries a check value nobody checked, the only case note explains.
func (lv *logView) hasUnchecked() bool {
	for _, row := range lv.rows {
		if row.line.MAC == invokelog.MACUnchecked {
			return true
		}
	}
	return false
}

// logKeys is the key line of the Logs screen for what has the focus; a narrow workspace gets the short form.
func (m *Model) logKeys() string {
	lv := m.logs
	if m.screen != screenNav && m.usable(0) < 60 {
		// The short form names the same keys with shorter words.
		switch {
		case lv.stats && lv.sessions && !lv.list.Searching():
			return "{up}/{down} scroll · {sessions} or {summary} rows · {filter} filter · {search} search · {area} section · {command} cmds · {help} help · {quit} quit"
		case lv.stats && !lv.list.Searching():
			return "{summary} rows · {filter} filter · {search} search · {area} section · {command} cmds · {help} help · {quit} quit"
		case lv.list.Searching():
			return "type to search · {up}/{down} move · {enter} keep · {back} clear"
		case lv.focus == logFocusBar && lv.bar != nil && lv.bar.Open():
			return "type to search · {up}/{down} move · {enter} choose · {back} close"
		case lv.focus == logFocusBar:
			return "{left}/{right} change · {enter} open · {next} next · {summary} summary · {sessions} MCP sessions · {search} search · {area} section · {back} back · {command} cmds · {help} help · {quit} quit"
		case lv.focus == logFocusTabs:
			return "{left}/{right} tab · {next} next · {filter} filter · {summary} summary · {sessions} MCP sessions · {search} search · {area} section · {back} back · {command} cmds · {help} help · {quit} quit"
		case lv.shownCount() == 0 || lv.heat:
			return "{next} next · {filter} filter · {summary} summary · {sessions} MCP sessions · {search} search · {area} section · {back} back · {command} cmds · {help} help · {quit} quit"
		}
		return "{enter} detail · {next} next · {filter} filter · {summary} summary · {sessions} MCP sessions · {search} search · {area} section · {back} back · {command} cmds · {help} help · {quit} quit"
	}
	if lv.stats && lv.sessions && m.screen != screenNav && !lv.list.Searching() {
		return "{up}/{down} scroll · {sessions} or {summary} or {back} back to the rows · {filter} filter · {search} search · {area} section · {command} cmds · {help} help · {quit} quit"
	}
	if lv.stats && m.screen != screenNav && !lv.list.Searching() {
		return "{summary} or {back} back to the rows · {filter} filter · {search} search · {area} section · {command} cmds · {help} help · {quit} quit"
	}
	switch {
	case m.screen == screenNav:
		return m.navKeys()
	case lv.list.Searching():
		return "type to search · {up}/{down} move · {enter} keep search · {back} clear search"
	case lv.focus == logFocusBar && lv.bar != nil && lv.bar.Open():
		return "type to search · {up}/{down} move · {enter} choose · {back} close"
	case lv.focus == logFocusBar:
		return "{left}/{right} change · {enter} open · {next} next field · {summary} summary · {sessions} MCP sessions · {search} search · {area} section · {back} back · {command} cmds · {help} help · {quit} quit"
	case lv.focus == logFocusTabs:
		return "{left}/{right} change tab · {next} next field · {filter} filter · {summary} summary · {sessions} MCP sessions · {search} search · {area} section · {back} back · {command} cmds · {help} help · {quit} quit"
	case lv.shownCount() == 0 || lv.heat:
		return "{next} next field · {left}/{right} change · {filter} filter · {summary} summary · {sessions} MCP sessions · {search} search · {area} section · {back} back · {command} cmds · {help} help · {quit} quit"
	}
	keys := "{enter} details · {next} next field · {left}/{right} change · {filter} filter · {summary} summary · {sessions} MCP sessions · {search} search · {area} section · {back} back · {command} cmds · {help} help · {quit} quit"
	if lv.list.Query() != "" {
		keys = strings.Replace(keys, "{back} back", "{back} clear search", 1)
	}
	return keys
}

// shownEntries are the parsed entries of the rows the filters and the search keep.
func (lv *logView) shownEntries() []invokelog.Entry {
	rows := lv.shownRows()
	entries := make([]invokelog.Entry, 0, len(rows))
	for _, row := range rows {
		if row.line.Parsed {
			entries = append(entries, row.line.Entry)
		}
	}
	return entries
}

// logStats is the summary of the rows the filters and the search keep: the table with a row per group and
// its layout for the workspace.
type logStats struct {
	table  table
	layout layout
	groups []invokelog.SizeGroup
}

func (m *Model) logStats() logStats {
	lv := m.logs
	entries := lv.shownEntries()
	s := logStats{groups: invokelog.Summarize(entries), table: table{columns: logColumnTitles(logStatColumns), rows: map[string][]string{}}}
	num := func(n int64) string { return strconv.FormatInt(n, 10) }
	for i, g := range s.groups {
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
		s.table.rows[strconv.Itoa(i)] = cells
	}
	_, width := m.fitIn(m.logRoom(), "  ")
	s.layout = s.table.layout(width)
	return s
}

// logStatLines draws the groups of s as many lines as fit in room; a last line says how many it left out.
func (m *Model) logStatLines(s logStats, room int) string {
	if len(s.groups) > room {
		room = max(room-1, 0)
	}
	var b strings.Builder
	for i := range s.groups {
		if i == room {
			b.WriteString(styles.hint.Render(tr(tLogMore, len(s.groups)-i)) + "\n")
			break
		}
		b.WriteString(m.listRowIn(m.logRoom(), false, s.table.line(s.table.rows[strconv.Itoa(i)], s.layout)) + "\n")
	}
	return b.String()
}

// logSessionStats is the table of the MCP sessions among the rows the filters and the search keep: a line
// for each session, its areas, and their tools, as the cells of the table, in the order of order.
type logSessionStats struct {
	table  table
	layout layout
	order  []string
}

func (m *Model) logSessionStats() logSessionStats {
	lv := m.logs
	entries := lv.shownEntries()
	s := logSessionStats{table: table{columns: logColumnTitles(logSessionColumns), rows: map[string][]string{}}}
	add := func(label string, t invokelog.ToolBytes) {
		name := strconv.Itoa(len(s.order))
		s.order = append(s.order, name)
		s.table.rows[name] = []string{label, strconv.Itoa(t.Calls), strconv.FormatInt(t.ReqBytes, 10),
			strconv.FormatInt(t.RespBytes, 10), strconv.FormatInt(t.Total(), 10),
			strconv.FormatInt(invokelog.EstimateTokens(t.Total()), 10)}
	}
	for _, session := range invokelog.SummarizeSessions(entries) {
		var sum invokelog.ToolBytes
		for _, a := range session.Areas {
			sum.Calls += a.Calls
			sum.ReqBytes += a.ReqBytes
			sum.RespBytes += a.RespBytes
		}
		add(tr(tLogSessionRow, m.logText(session.Session)), sum)
		for _, a := range session.Areas {
			add("  "+string(a.Area), a.ToolBytes)
			for _, t := range a.Tools {
				add("    "+orDash(m.logText(t.Operation)), t)
			}
		}
	}
	_, width := m.fitIn(m.logRoom(), "  ")
	s.layout = s.table.layout(width)
	return s
}

// sessionRoom is how many lines the session summary has room for between the frame of the Logs screen.
func (m *Model) sessionRoom() int {
	head, foot := m.logsFrame()
	return m.height - strings.Count(head, "\n") - strings.Count(foot, "\n") - 1
}

// scrollSessions moves the shown part of the session summary; the view clamps it again for its own room.
func (m *Model) scrollSessions(key tea.KeyPressMsg) {
	lv := m.logs
	room := max(m.sessionRoom()-1, 1)
	last := max(len(m.logSessionStats().order)-room, 0)
	by := 0
	switch {
	case keyIs(key, actUp):
		by = -1
	case keyIs(key, actDown):
		by = 1
	case keyIs(key, actPageUp):
		by = -room
	case keyIs(key, actPageDown):
		by = room
	}
	switch {
	case keyIs(key, actHome):
		lv.scroll = 0
	case keyIs(key, actEnd):
		lv.scroll = last
	default:
		lv.scroll = min(max(lv.scroll+by, 0), last)
	}
}

// sessionStatLines draws the lines of s from the scroll position as many as fit in room, or says that the
// rows shown hold no MCP session; a last line says how many lines it left below.
func (m *Model) sessionStatLines(s logSessionStats, room int) string {
	if len(s.order) == 0 {
		return render(lipgloss.NewStyle(), m.logRoom(), tr(tLogNoSession)) + "\n"
	}
	start := min(max(m.logs.scroll, 0), max(len(s.order)-1, 0))
	if len(s.order)-start > room {
		room = max(room-1, 0)
	}
	var b strings.Builder
	for i := start; i < len(s.order); i++ {
		if i-start == room {
			b.WriteString(styles.hint.Render(tr(tLogMore, len(s.order)-i)) + "\n")
			break
		}
		b.WriteString(m.listRowIn(m.logRoom(), false, s.table.line(s.table.rows[s.order[i]], s.layout)) + "\n")
	}
	return b.String()
}

// logDetailView shows every field of one entry, and its status with the reason.
func (m *Model) logDetailView() string {
	lv := m.logs
	if lv.detail < 0 || lv.detail >= len(lv.rows) {
		return m.keyHint(keysBackOnly)
	}
	head, lines := m.logDetailLayout(lv.rows[lv.detail])
	keys := keysLogDetail
	// The lines after the last field are the key line and the notes, and at least the empty line the view
	// ends in.
	tail := func(keys string) int { return strings.Count(m.keyHint(keys)+m.notes(), "\n") + 1 }
	room := m.height - strings.Count(head, "\n") - tail(keys)
	position := ""
	if total := len(lines); total > room {
		keys = keysScroll + keys
		room = max(m.height-strings.Count(head, "\n")-tail(keys)-1, 1)
		lv.page = room
		lines = lv.view.window(lines, room, false)
		position = m.wrapped(styles.hint, lv.view.position(total, room)) + "\n"
	} else {
		lv.view.reset()
	}
	return head + strings.Join(lines, "\n") + "\n" + position + m.keyHint(keys)
}

// logDetailLayout is the heading and the laid out fields of the detail screen of one entry. The fields are
// laid out first and shown from the scroll position on, so a long message scrolls and nothing is cut.
func (m *Model) logDetailLayout(row logRow) (string, []string) {
	var b strings.Builder
	head := m.wrapped(styles.title, m.logDetailTitle(row)) + "\n\n"
	m.formCol = m.labelColumn(logLabel("status"), logLabel("UTC file"), logLabel("sequence"), logLabel("day"), logLabel("kind"),
		logLabel("through"), logLabel("way"), logLabel("client"), logLabel("tool"), logLabel("connection"),
		logLabel("effect"), logLabel("result"), logLabel("duration"), logLabel("request"), logLabel("response"))
	defer func() { m.formCol = 0 }()
	for _, f := range m.logDetailFields(row) {
		b.WriteString(m.formRow(false, f.label, f.value) + "\n")
	}
	return head, strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
}

// logDetailKeys is the key text of the detail screen: a text longer than the room gains the scroll keys.
func (m *Model) logDetailKeys() string {
	lv := m.logs
	if lv.detail < 0 || lv.detail >= len(lv.rows) {
		return keysBackOnly
	}
	head, lines := m.logDetailLayout(lv.rows[lv.detail])
	if len(lines) > m.height-strings.Count(head, "\n")-strings.Count(m.notes(), "\n")-1 {
		return keysScroll + keysLogDetail
	}
	return keysLogDetail
}

// logDetailTitle is the heading of the detail screen of one entry.
func (m *Model) logDetailTitle(row logRow) string {
	if row.line.Parsed && !row.line.Entry.Time.IsZero() {
		return tr(tLogEntryTime, row.line.Entry.Time.In(time.Local).Format(logDateLayout+" 15:04:05 -0700"))
	}
	return tr(tLogEntryFile, row.date)
}

// logDetailFields are the labelled values of the detail screen of one entry, every one made safe by logText
// or taken from a cell that was.
func (m *Model) logDetailFields(row logRow) []detailFact {
	e := row.line.Entry
	var fields []detailFact
	field := func(label, value string) { fields = append(fields, detailFact{logLabel(label), value}) }
	field("status", row.status.label()+" ("+m.logReason(row.line)+")")
	field("UTC file", row.date)
	if row.line.Parsed {
		field("sequence", strconv.FormatUint(e.Seq, 10))
	}
	switch {
	case !row.line.Parsed:
		field("day", row.date)
	case e.Kind == invokelog.KindCut:
		field("kind", tr(tLogRetentionCut))
		if e.Cut != nil {
			field("through", tr(tLogCutThrough, e.Cut.ThroughSeq))
		}
	default:
		field("way", row.values[logFilterWay])
		client := "-"
		if e.Client != nil {
			client = strings.TrimSpace(m.logText(e.Client.Name) + " " + m.logText(e.Client.Version))
		}
		field("client", orDash(client))
		tool := row.values[logFilterTool]
		if e.Version > 0 {
			tool += tr(tLogToolVersion, e.Version)
		}
		field("tool", tool)
		if e.Tool != "" {
			field("requested tool", m.logText(e.Tool))
		}
		field("connection", row.values[logFilterConnection])
		field("effect", row.values[logFilterEffect])
		field("result", orDash(m.logText(e.Result)))
		field("duration", formatDuration(e.DurationMS))
		field("request", formatBytes(e.ReqBytes))
		field("response", formatBytes(e.RespBytes))
	}
	return fields
}

// logReason says why a line has its status.
func (m *Model) logReason(line invokelog.LineResult) string {
	var reasons []string
	switch line.Chain {
	case invokelog.ChainUnparsed:
		reasons = append(reasons, tr(tLogReasonUnparsed))
	case invokelog.ChainGenesis:
		reasons = append(reasons, tr(tLogReasonGenesis))
	case invokelog.ChainNext:
		reasons = append(reasons, tr(tLogReasonNext))
	case invokelog.ChainPrevious:
		reasons = append(reasons, tr(tLogReasonPrevious))
	}
	if line.Missing > 0 {
		reasons = append(reasons, tr(tLogReasonMissing, line.Missing))
	}
	if line.Parsed {
		switch line.MAC {
		case invokelog.MACInvalid:
			reasons = append(reasons, tr(tLogReasonInvalid))
		case invokelog.MACValid:
			if len(reasons) == 0 {
				reasons = append(reasons, tr(tLogReasonValid))
			}
		case invokelog.MACUnchecked:
			note := m.logs.note
			if note == "" {
				note = tr(tLogReasonUnchecked)
			}
			reasons = append(reasons, note)
		case invokelog.MACNone:
			reasons = append(reasons, tr(tLogReasonNoKey))
		}
	}
	return strings.Join(reasons, "; ")
}
