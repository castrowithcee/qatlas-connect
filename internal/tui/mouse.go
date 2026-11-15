package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/charmbracelet/x/ansi"
)

// The mouse reaches the editor through a list of click targets that the drawing of a frame fills as it goes:
// every place that draws something clickable says where on the screen it stands and which key press the click
// stands for. A click then does what that key does, through the same handlers, so every check the key meets
// (the admin session, the confirmations, the question before unsaved input is dropped) stays in force, and a
// click is never stronger than its key. A row is only marked by the first click, so that a click that meant to
// scroll or to take the focus opens nothing; the click on the marked row is the press. Nothing is drawn from the
// list and nothing is stored beyond one frame.

// hit is one click target: the cells [x0, x1) by [y0, y1) of the screen and what a click on them does.
type hit struct {
	x0, y0, x1, y1 int
	act            func() tea.Cmd
}

// addHit registers a click target of the workspace. A screen drawn as the background of a dialog has none: the
// dialog is what a click reaches then.
func (m *Model) addHit(x0, y0, x1, y1 int, act func() tea.Cmd) {
	if m.backdrop || m.tooSmall || x1 <= x0 || y1 <= y0 {
		return
	}
	m.hits = append(m.hits, hit{x0, y0, x1, y1, act})
}

// addFrameHit registers a click target of the tab line or the option bar.
func (m *Model) addFrameHit(x0, y0, x1, y1 int, act func() tea.Cmd) {
	if m.backdrop || m.tooSmall || x1 <= x0 || y1 <= y0 {
		return
	}
	m.frameHits = append(m.frameHits, hit{x0, y0, x1, y1, act})
}

// addBarHit registers a tab of the tab line or the option bar. It also stands under a dialog, whose backdrop is
// the frame with these bars: whether a click on one may go on is for leaveForBar to say.
func (m *Model) addBarHit(x0, y0, x1, y1 int, act func() tea.Cmd) {
	if m.tooSmall || x1 <= x0 || y1 <= y0 {
		return
	}
	m.barHits = append(m.barHits, hit{x0, y0, x1, y1, act})
}

// markDialog notes the cells a dialog drawn from row top and column left covers.
func (m *Model) markDialog(dialog []string, top, left int) {
	w := 0
	for _, line := range dialog {
		w = max(w, ansi.StringWidth(line))
	}
	m.dialogBox = hit{x0: left, y0: top, x1: left + w, y1: top + len(dialog)}
}

// addZone registers a focus zone: the free area of a part of the screen that a key press can focus. A click on
// it does only what tabbing there does, never more, and a zone ranks below every other target, so that a click
// on an element inside it stays that element's click.
func (m *Model) addZone(x0, y0, x1, y1 int, act func() tea.Cmd) {
	if m.backdrop || m.tooSmall || x1 <= x0 || y1 <= y0 {
		return
	}
	m.zones = append(m.zones, hit{x0, y0, x1, y1, act})
}

// workspaceTop is the screen row of the first line of the workspace: below the header, the tab line, the option
// bar of an area that has one, and the rule.
func (m *Model) workspaceTop() int {
	top := len(m.headerLines()) + 2
	if m.hasSubTabs() {
		top++
	}
	return top
}

// hitTop is the screen row of the first line after header, a text the workspace starts with.
func (m *Model) hitTop(header string) int { return m.workspaceTop() + strings.Count(header, "\n") }

// hitRows makes the rows [start, end) of a windowed list clickable. The rows stand one under the other from
// row top on, each as high as row draws it, in the first width cells.
func (m *Model) hitRows(top, width, start, end int, row func(i int) string, act func(i int) tea.Cmd) {
	y := top
	for i := start; i < end; i++ {
		height := strings.Count(row(i), "\n") + 1
		m.addHit(0, y, width, y+height, func() tea.Cmd { return act(i) })
		y += height
	}
}

// mouseEnabled reports whether the editor takes mouse input.
func (m *Model) mouseEnabled() bool { return m.prefs.MouseEnabled() }

// mouseMode is the mouse mode the view asks the terminal for: cell motion while the editor takes the mouse.
func (m *Model) mouseMode() tea.MouseMode {
	if m.mouseEnabled() {
		return tea.MouseModeCellMotion
	}
	return tea.MouseModeNone
}

// wheelText is how many rows one notch of the wheel moves in a text area; elsewhere it is one step.
const wheelText = 3

// wheelClamp makes wrap stop at the ends while the wheel presses a key: the wheel never jumps from the last
// row to the first or the other way round, the keys keep their wraparound.
var wheelClamp bool

// updateMouse handles one mouse event. It acts only on a press of the left button and on the wheel; motion and
// release do nothing. A terminal too small for the screen, an editor with the mouse switched off, and the
// capture of a new key take no mouse at all.
func (m *Model) updateMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg, tea.MouseWheelMsg:
	default:
		return nil
	}
	event := msg.Mouse()
	if m.quitting || !m.mouseEnabled() || m.terminalTooSmall() || m.keyCapture {
		return nil
	}
	wheel := event.Button == tea.MouseWheelUp || event.Button == tea.MouseWheelDown
	if !wheel && event.Button != tea.MouseLeft {
		return nil
	}
	// The targets are those of the state the event meets: the frame drawn after the last message has them, and
	// only a state changed since then needs a new draw. The event itself may change the state.
	if !m.drawn {
		m.render()
	}
	// The frame fits (tooSmall is clear below), so the first key of the event skips the check of the size. What
	// the event does leaves the frame stale.
	m.verified = !m.tooSmall
	defer func() { m.verified, m.drawn = false, false }()
	if m.tooSmall {
		return nil
	}
	// The targets of the frame go through the kit map; the acts stay with the application, which runs the
	// click as the key or the tab click it stands for.
	if wheel {
		targets, _ := m.mouseTargets(false)
		if !kittui.NewMouseMap(targets, true).Handle(msg).Handled || (m.dd != nil && !m.infoOpen) {
			return nil
		}
		return m.wheel(event)
	}
	if m.infoOpen {
		// The explanation closes the way esc closes it; a click on a tab then goes on.
		m.infoOpen = false
		cmd, _ := m.clickBar(msg)
		return cmd
	}
	if m.dd != nil {
		return m.clickDropdown(msg)
	}
	targets, acts := m.mouseTargets(false)
	return m.runTarget(kittui.NewMouseMap(targets, true).Handle(msg), acts)
}

// clickBar is the click on a tab of the tab line or the option bar, if the event is one. A dialog covers the
// tabs under it.
func (m *Model) clickBar(msg tea.MouseMsg) (tea.Cmd, bool) {
	targets, acts := m.mouseTargets(true)
	r := kittui.NewMouseMap(targets, true).Handle(msg)
	if !r.Handled || acts[targetIndex(r)] == nil {
		return nil, false
	}
	return m.runTarget(r, acts), true
}

// mouseTargets is the bridge from the click targets of the frame to kit targets. Each old target becomes one
// kit target whose Item (or, for a zone, Zone) is its index in acts; the kit map finds the target, the act
// stays the application's. Later stages replace the bridged targets of a part by the part's own. Stacking
// follows from the order: a zone lies below every element, a tab bar target below the frame and workspace
// targets (a dialog over a tab takes the click there), and the explanation leaves the tab bars alone.
func (m *Model) mouseTargets(barsOnly bool) ([]kittui.Target, []func() tea.Cmd) {
	var targets []kittui.Target
	var acts []func() tea.Cmd
	add := func(h hit, zone bool) {
		id := strconv.Itoa(len(acts))
		t := kittui.Target{Rect: kittui.Rect{X: h.x0, Y: h.y0, Width: h.x1 - h.x0, Height: h.y1 - h.y0}}
		if zone {
			t.Zone = kittui.FocusZone(id)
		} else {
			t.Item = id
		}
		targets = append(targets, t)
		acts = append(acts, h.act)
	}
	if !barsOnly {
		for _, h := range m.zones {
			add(h, true)
		}
	}
	for _, h := range m.barHits {
		add(h, false)
	}
	if d := m.dialogBox; d.x1 > d.x0 && d.y1 > d.y0 {
		// The dialog covers the tabs under it: a click on its text reaches no tab and does nothing.
		add(d, false)
		acts[len(acts)-1] = nil
	}
	if !barsOnly {
		for _, h := range m.frameHits {
			add(h, false)
		}
		for _, h := range m.hits {
			add(h, false)
		}
	}
	return targets, acts
}

// targetIndex is the index in acts that a kit result names: its item, or for a bare zone the zone.
func targetIndex(r kittui.MouseResult) int {
	id := r.Item
	if id == "" {
		id = string(r.Zone)
	}
	i, err := strconv.Atoi(id)
	if err != nil {
		return -1
	}
	return i
}

// runTarget runs the act of a kit result as the click it stands for: a bare zone focuses, an item acts.
func (m *Model) runTarget(r kittui.MouseResult, acts []func() tea.Cmd) tea.Cmd {
	i := targetIndex(r)
	if i < 0 || i >= len(acts) || acts[i] == nil {
		return nil
	}
	m.touchAdminSessionIfActive()
	return acts[i]()
}

// wheelScreens are the screens whose up and down keys move over rows; the wheel is those keys there.
func (m *Model) wheelScreens() bool {
	switch m.screen {
	case screenList, screenForm, screenPicker, screenProviders, screenHelp, screenLogs, screenLogDetail,
		screenTargets, screenPaths, screenTemplate, screenFindings, screenCommand, screenVersions:
		return true
	}
	return m.textScreen()
}

// textScreen reports whether what the screen shows is a plain text area, which the wheel moves by wheelText
// rows. The steps of a transfer or rename that ask for input or move over rows are not.
func (m *Model) textScreen() bool {
	if m.infoOpen {
		return true
	}
	switch m.screen {
	case screenHelp, screenSelftest, screenLogDetail:
		return true
	case screenConfirm:
		return m.approvalDetail != ""
	case screenRename:
		return m.rename != nil && m.rename.plan != nil
	case screenTransfer:
		if e := m.export; e != nil {
			return e.step == exportPreview || e.step == exportResult
		}
		if e := m.imp; e != nil {
			return e.step >= importPreview
		}
	}
	return false
}

// atListTop reports whether up would leave the rows of the list for its filter bar or the tab line.
func (m *Model) atListTop() bool {
	if m.screen != screenList {
		return false
	}
	if m.barFocus != 0 {
		return true
	}
	if m.section == sectionOverview {
		return m.overviewCursor <= 0
	}
	return m.list.cursor <= 0
}

// wheel scrolls: a notch is one press of up or down (three in a text area), and over the list of the sections
// of a form it is a press of pgup or pgdown, which jump from section to section. On the tab line a notch down
// goes into the content and nothing leads back out of it.
func (m *Model) wheel(event tea.Mouse) tea.Cmd {
	up := event.Button == tea.MouseWheelUp
	if !m.infoOpen {
		// A text pane beside the screen scrolls under the wheel without taking the focus or moving the rows.
		for _, pane := range []*paneScroll{&m.detailPane, &m.infoPane} {
			if (pane == &m.detailPane && (m.screen == screenList || m.screen == screenNav) && m.paneWidth() > 0 || pane == &m.infoPane && m.paneShown()) && pane.at(event.X, event.Y) {
				m.touchAdminSessionIfActive()
				pane.wheel(up)
				return nil
			}
		}
	}
	if m.screen == screenNav && !m.infoOpen {
		if up {
			return nil
		}
		m.touchAdminSessionIfActive()
		var cmds []tea.Cmd
		// The option bar of an area is a stop before its content.
		for range 2 {
			if m.screen != screenNav {
				break
			}
			cmds = append(cmds, m.press(tea.KeyPressMsg{Code: tea.KeyDown}))
		}
		return tea.Batch(cmds...)
	}
	if !m.wheelScreens() || (up && !m.infoOpen && m.atListTop()) {
		return nil
	}
	m.touchAdminSessionIfActive()
	key, times := rune(tea.KeyDown), 1
	if up {
		key = tea.KeyUp
	}
	if m.textScreen() {
		times = wheelText
	}
	if m.screen == screenForm && !m.infoOpen && event.X < m.sectionListWidth {
		key = tea.KeyPgDown
		if up {
			key = tea.KeyPgUp
		}
	}
	wheelClamp = true
	defer func() { wheelClamp = false }()
	var cmds []tea.Cmd
	for range times {
		cmds = append(cmds, m.press(tea.KeyPressMsg{Code: key}))
	}
	return tea.Batch(cmds...)
}

// press hands a key to the editor as if it had been typed. A mouse event never makes a character: a letter
// is not passed on where the screen reads text.
func (m *Model) press(key tea.KeyPressMsg) tea.Cmd {
	if key.Text != "" && m.typing() {
		return nil
	}
	_, cmd := m.Update(key)
	return cmd
}

// pressName is press for a key named as a footer names it.
func (m *Model) pressName(name string) tea.Cmd {
	key, ok := keyPress(name)
	if !ok {
		return nil
	}
	return m.press(key)
}

// footerSpan is one key group of a footer line: the cells it stands in and the key it names.
type footerSpan struct {
	x0, x1 int
	key    string
}

// footerLine is a footer line as drawn, and where its key groups stand.
type footerLine struct {
	plain string
	spans []footerSpan
}

// footerKey is the key a footer group opens with ("y delete" names y). Of alternatives ("n/esc") the first
// stands for the group. A group that opens with anything else, such as "1-6" or "type to filter", has none.
func footerKey(group string) string {
	word, _, _ := strings.Cut(group, " ")
	word, _, _ = strings.Cut(word, "/")
	if _, ok := keyPress(word); ok {
		return word
	}
	return ""
}

// hitDialog makes the keys of the footer lines of a dialog clickable: they are its buttons. lines are the
// rows of the dialog text, drawn from column left+2 of row top+1 on, inside its frame.
func (m *Model) hitDialog(lines []string, log []footerLine, top, left int) {
	m.hitFooters(lines, log, top+1, left+2)
}

// hitFooters makes the key groups of the footer lines found among lines clickable. lines are drawn from screen
// row y0 and column x0 on.
func (m *Model) hitFooters(lines []string, log []footerLine, y0, x0 int) {
	used := make([]bool, len(lines))
	for _, footer := range log {
		for i, line := range lines {
			if used[i] || ansi.Strip(line) != footer.plain {
				continue
			}
			used[i] = true
			for _, span := range footer.spans {
				if span.key == "" {
					continue
				}
				name := span.key
				m.addHit(x0+span.x0, y0+i, x0+span.x1, y0+i+1, func() tea.Cmd { return m.pressName(name) })
			}
			break
		}
	}
}

// dialogOrigin is the top row and left column of a dialog centred in a screen of width by height.
func dialogOrigin(dialog []string, width, height int) (top, left int) {
	dialogWidth := 0
	for _, line := range dialog {
		dialogWidth = max(dialogWidth, ansi.StringWidth(line))
	}
	return max((height-len(dialog))/2, 0), max((width-dialogWidth)/2, 0)
}

// navSpan is the cells one entry of the tab line or the option bar stands in.
type navSpan struct{ x0, x1 int }

// spanItems places the items of a bar one after the other from column start on, with sep between them, and
// cuts the places to width cells.
func spanItems(items []string, start int, sep string, width int) []navSpan {
	spans := make([]navSpan, len(items))
	x := start
	for i, item := range items {
		w := ansi.StringWidth(item)
		spans[i] = navSpan{min(x, width), min(x+w, width)}
		x += w + ansi.StringWidth(sep)
	}
	return spans
}

// barLeaveSteps bounds the esc presses a click on a bar makes, so a screen whose esc changes nothing cannot
// loop; the deepest stack of screens is a few levels.
const barLeaveSteps = 8

// backShown reports whether esc leads one level back from the screen shown now, which is where the back button
// stands. It is false on the top level of an area (the tab line, a list, Logs, the page form of a setting),
// whose esc only returns to the tab line, and wherever a question, a dialog or an overlay is open and esc
// answers that instead.
func (m *Model) backShown() bool {
	if m.backdrop || m.infoOpen || m.keyCapture || m.dd != nil || m.testResultShown() {
		return false
	}
	if _, ok := keyPress(keyLabel(actBack)); !ok {
		return false
	}
	switch m.screen {
	case screenForm:
		if m.payloadRemoveAsk || m.payloadSaving || (m.wizard != nil && m.wizard.saving) {
			return false
		}
		return m.wizard != nil || !m.section.isPage()
	case screenSummary:
		return m.wizard != nil && !m.wizard.saving
	case screenConfirm:
		return (m.approvalDetail != "" && !m.approveAllConfirm) || (m.tokenDetail != "" && m.tokenRevoke == "")
	case screenHelp:
		return !m.helpOverlay()
	case screenTargets:
		return !m.targetRemove
	case screenPaths:
		return !m.pathRemove
	case screenTemplate:
		return m.template != nil
	case screenTransfer:
		// The questions about approving and deleting are answered with esc, not left; a write in flight
		// ignores it.
		if e := m.imp; e != nil {
			return e.working == "" && e.step != importApprove && e.step != importDelete
		}
		return m.export != nil && !m.export.writing
	case screenSelftest:
		// A running test is ended by esc; once it is ending, esc does nothing.
		return m.st != nil && !m.st.cancelling
	case screenPicker, screenProviders, screenLogDetail, screenFindings, screenVersions:
		return true
	}
	return false
}

// pressBack is a press of the key bound to esc; the click on the back button and the climb out of a deeper
// screen use it so they meet every rule the key meets.
func (m *Model) pressBack() tea.Cmd {
	key, ok := keyPress(keyLabel(actBack))
	if !ok {
		return nil
	}
	return m.press(key)
}

// registerBack makes the back button of the header clickable.
func (m *Model) registerBack(span navSpan) {
	m.addFrameHit(span.x0, 0, span.x1, 1, m.pressBack)
}

// leaveForBar prepares the screen for a click on the tab line or the option bar: it goes the way esc goes, one
// level after the other, up to a screen the keys reach the bars from. A typed line ends and an overlay without
// a question closes as esc ends them; the form or the guided setup is left as esc leaves it, and the question
// esc raises on the way (unsaved input) is the end of the click. A question, a dialog that asks for a
// passphrase, a connection test result, a save in flight and the capture of a key are never answered by a
// click, so a click on a tab does nothing there. It reports whether the click may go on.
func (m *Model) leaveForBar() (cmd tea.Cmd, ok bool) {
	var cmds []tea.Cmd
	for range barLeaveSteps {
		if m.barAsked() {
			break
		}
		switch m.screen {
		case screenNav:
			return tea.Batch(cmds...), true
		case screenList, screenLogs:
			if !m.typing() {
				return tea.Batch(cmds...), true
			}
		case screenForm, screenSummary, screenProviders:
			if m.wizard != nil || m.screen == screenForm {
				if m.payloadRemoveAsk || m.payloadSaving || (m.wizard != nil && m.wizard.saving) {
					return tea.Batch(cmds...), false
				}
				cmds = append(cmds, m.requestLeave())
				return tea.Batch(cmds...), m.screen != screenLeave
			}
		}
		if !m.typing() && !m.backShown() && m.screen != screenHelp {
			break
		}
		cmds = append(cmds, m.pressBack())
	}
	return tea.Batch(cmds...), false
}

// barAsked reports whether a click on a tab must leave the screen as it is: a question or a dialog with a
// passphrase is open, a connection test result waits, or a key is being captured.
func (m *Model) barAsked() bool {
	if m.keyCapture || m.questionScreen() || m.testResultShown() {
		return true
	}
	switch m.screen {
	case screenSecret, screenVaultOffer, screenAdminAuth, screenVaultUnlock, screenLeave:
		return true
	}
	return false
}

// clickArea is a click on an area of the tab line.
func (m *Model) clickArea(a area) tea.Cmd {
	lead, ok := m.leaveForBar()
	if !ok {
		return lead
	}
	return tea.Batch(lead, m.previewArea(a))
}

// clickSubTab is a click on a part of the option bar.
func (m *Model) clickSubTab(s section) tea.Cmd {
	lead, ok := m.leaveForBar()
	if !ok {
		return lead
	}
	cmd := m.previewSection(s)
	m.navSub = true
	return tea.Batch(lead, cmd)
}

// zoneBar is the click on the free area of the tab line or the option bar: it leaves the screen the way a click
// on a tab does, and hands the focus to the bar without changing the section.
func (m *Model) zoneBar(sub bool) tea.Cmd {
	lead, ok := m.leaveForBar()
	if !ok {
		return lead
	}
	if m.screen != screenNav {
		m.focusNav()
	}
	m.navSub = sub && m.hasSubTabs()
	return lead
}

func (m *Model) zoneTabLine() tea.Cmd   { return m.zoneBar(false) }
func (m *Model) zoneOptionBar() tea.Cmd { return m.zoneBar(true) }

// zoneRows is the click on the free area of the rows of a list or the Overview: the rows take the focus, as
// tab or enter from the bars gives it. On the page of a setting that is entering the page. A typed filter is
// left alone.
func (m *Model) zoneRows() tea.Cmd {
	switch m.screen {
	case screenNav:
		return m.focusList()
	case screenList:
	default:
		return nil
	}
	if m.list.editing {
		return nil
	}
	m.barFocus, m.noticeFocus = 0, false
	m.leaveDetail()
	return nil
}

// zoneFilterBar is the click on the free area of the filter bar of a list: the first field takes the focus,
// as tab gives it, unless a field of the bar has it already.
func (m *Model) zoneFilterBar() tea.Cmd {
	if m.screen == screenNav {
		if cmd := m.focusList(); m.screen != screenList {
			return cmd
		}
	}
	if m.screen != screenList || m.list.editing || !m.listBarShown() {
		return nil
	}
	m.leaveDetail()
	m.noticeFocus = false
	if m.barFocus == 0 {
		m.barFocus = 1
	}
	return nil
}

// zoneList registers the zones of a list screen drawn as out with footer below the table: the table, from its
// title to the end of its frame, and the filter bar inside it. A filter being typed has none.
func (m *Model) zoneList(out, footer string) {
	if m.list.editing {
		return
	}
	top := m.workspaceTop()
	end := top + strings.Count(out, "\n") - strings.Count(footer, "\n")
	if m.tableFramed() {
		end++
	}
	m.addZone(0, top, m.listWidth(), end, m.zoneRows)
	if m.listBarShown() {
		row := top + m.listBarOffset()
		m.addZone(0, row, m.listWidth(), row+1, m.zoneFilterBar)
	}
}

// registerBars makes the tab line and the option bar clickable. The guided setup shows the tab line only; its
// option bar row stays empty, and the free areas of the bars are not targets there.
func (m *Model) registerBars() {
	if m.tooSmall {
		return
	}
	row := len(m.headerLines())
	setup := m.wizard != nil
	if !setup {
		m.addZone(0, row, m.termWidth, row+1, m.zoneTabLine)
		if m.hasSubTabs() {
			m.addZone(0, row+1, m.termWidth, row+2, m.zoneOptionBar)
		}
	}
	areas := m.navTabs()
	for s, span := range areas.spans {
		a := area(s)
		m.addBarHit(span.x0, row, span.x1, row+1, func() tea.Cmd { return m.clickArea(a) })
	}
	if setup || !m.hasSubTabs() {
		return
	}
	for _, sub := range m.subNavSpans() {
		s := sub.section
		m.addBarHit(sub.x0, row+1, sub.x1, row+2, func() tea.Cmd { return m.clickSubTab(s) })
	}
}

// barSpec says where one field of an option bar stands in a drawn line and what it offers: its text, the width
// of what stands before its alternatives, the alternatives with the active one drawn in brackets, and what
// choosing the i-th does.
type barSpec struct {
	field  string
	prefix int
	values []string
	active int
	choose func(i int) tea.Cmd
	// id names the field for the list that opens under it (see noteField); "" for a field with none.
	id string
	// whole, when set, makes the whole field one target; the field has no alternatives then.
	whole func() tea.Cmd
}

// hitBar makes the alternatives of the fields of a bar clickable. lines are the drawn rows of the bar from
// screen row top on; a field is found by its text, so a bar that was shortened to a summary has no targets.
func (m *Model) hitBar(lines []string, top int, specs []barSpec) {
	for _, spec := range specs {
		for row, line := range lines {
			plain := ansi.Strip(line)
			at := strings.Index(plain, spec.field)
			if at < 0 {
				continue
			}
			x := ansi.StringWidth(plain[:at]) + spec.prefix
			if spec.id != "" {
				m.noteField(spec.id, x-spec.prefix, top+row)
			}
			if spec.whole != nil {
				m.addHit(x-spec.prefix, top+row, x-spec.prefix+ansi.StringWidth(spec.field), top+row+1, spec.whole)
			}
			for i, value := range spec.values {
				w := ansi.StringWidth(value)
				if i == spec.active {
					w += 2
				}
				m.addHit(x, top+row, x+w, top+row+1, func() tea.Cmd { return spec.choose(i) })
				x += w + 1
			}
			break
		}
	}
}

// listBarOffset is the number of lines above the filter bar of the list screen: its title, the gap under it
// and the top border of the frame.
func (m *Model) listBarOffset() int {
	if m.tableFramed() {
		return m.titleRows() + 1
	}
	return 1
}

// hitList makes the rows of the list screen, and its filter bar, clickable.
func (m *Model) hitList(header string, start, end int, row func(i int) string) {
	if m.listBarShown() && !m.list.editing {
		m.hitBarFields(m.workspaceTop() + m.listBarOffset())
	}
	m.hitRows(m.hitTop(header), m.listWidth(), start, end, row, m.clickListRow)
}

// markOrAct is the click on a row of a list: it marks the row, and only a click on the row that already was
// marked, with the focus on the rows, does act. focused says the rows had the focus before the click.
func markOrAct(cursor *int, i int, focused bool, act func() tea.Cmd) tea.Cmd {
	if !focused || *cursor != i {
		*cursor = i
		return nil
	}
	return act()
}

// clickListRow is a click on a row of the list: it marks the row, and a click on the marked row does what enter
// does there, which opens an entry. A group folds at the first click, which is harmless and undone by the next.
// A filter being typed is only kept.
func (m *Model) clickListRow(i int) tea.Cmd {
	if i < 0 || i >= len(m.list.matches) {
		return nil
	}
	focused := m.screen == screenList && m.barFocus == 0 && !m.list.editing && !m.detailFocused()
	switch m.screen {
	case screenNav:
		if cmd := m.focusList(); m.screen != screenList {
			return cmd
		}
	case screenList:
	default:
		return nil
	}
	m.barFocus = 0
	m.leaveDetail()
	if m.list.editing {
		m.list.cursor = i
		m.list.stopFilter()
		return nil
	}
	enter := func() tea.Cmd { return m.press(tea.KeyPressMsg{Code: tea.KeyEnter}) }
	if isGroupRow(m.list.matches[i]) {
		m.list.cursor = i
		return enter()
	}
	return markOrAct(&m.list.cursor, i, focused, enter)
}

// hitOverview makes the selectable items of the Overview, its cards and lines, clickable.
func (m *Model) hitOverview() {
	top := m.workspaceTop()
	for i, h := range m.overviewHits {
		m.addHit(h.x0, top+h.y0, h.x1, top+h.y1, func() tea.Cmd { return m.clickOverview(i) })
	}
}

// clickOverview is a click on an item of the Overview: it marks the item, and a click on the marked one opens
// its area, as enter does.
func (m *Model) clickOverview(i int) tea.Cmd {
	focused := m.screen == screenList
	switch m.screen {
	case screenNav:
		m.focusList()
	case screenList:
	default:
		return nil
	}
	return markOrAct(&m.overviewCursor, i, focused, func() tea.Cmd { return m.press(tea.KeyPressMsg{Code: tea.KeyEnter}) })
}

// hitPicker makes the values of the picker clickable. A click marks a value; a click on the marked one ticks it
// in a picker of ticks, as space does, and chooses it in any other, as enter does. Its search line takes the
// keys of the rows, so the rows always have the focus. The info mark of a value opens its explanation.
func (m *Model) hitPicker(header string, start, end int) {
	m.hitRows(m.hitTop(header), m.usable(0), start, end, m.pickerRow, func(i int) tea.Cmd {
		if m.screen != screenPicker || i >= len(m.picker.matches) {
			return nil
		}
		return markOrAct(&m.picker.cursor, i, true, func() tea.Cmd {
			if m.pickerMarks != nil {
				return m.updatePicker(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			}
			return m.press(tea.KeyPressMsg{Code: tea.KeyEnter})
		})
	})
	y := m.hitTop(header)
	for i := start; i < end; i++ {
		text := m.pickerRow(i)
		m.hitInfoMark(m.pickerRowText(i), y, m.boxX(), func() tea.Cmd {
			m.picker.cursor = i
			m.openInfo()
			return nil
		})
		y += strings.Count(text, "\n") + 1
	}
}

// hitInfoMark makes the info mark that ends a line of text clickable: it opens the explanation, as the info
// key does. The text starts on screen row y, in column x0.
func (m *Model) hitInfoMark(text string, y, x0 int, act func() tea.Cmd) {
	mark := strings.TrimSpace(infoMark)
	for row, line := range strings.Split(text, "\n") {
		plain := strings.TrimRight(ansi.Strip(line), " ")
		if !strings.HasSuffix(plain, mark) {
			continue
		}
		x := x0 + ansi.StringWidth(strings.TrimSuffix(plain, mark))
		m.addHit(x, y+row, x+len(mark), y+row+1, act)
	}
}

// hitTable makes the rows of a windowed list of a screen clickable: a click marks a row, and a click on the
// marked row is a press of enter on it.
func (m *Model) hitTable(l *filterList, on screen, header string, start, end int, row func(i int) string) {
	m.hitRows(m.hitTop(header), m.usable(0), start, end, row, func(i int) tea.Cmd {
		if m.screen != on || i >= len(l.matches) {
			return nil
		}
		return markOrAct(&l.cursor, i, true, func() tea.Cmd { return m.press(tea.KeyPressMsg{Code: tea.KeyEnter}) })
	})
}

// hitSelect makes the rows of a windowed list of a screen clickable by choosing the row only; what enter would
// do on it is left to the key.
func (m *Model) hitSelect(l *filterList, on screen, header string, start, end int, row func(i int) string) {
	m.hitRows(m.hitTop(header), m.usable(0), start, end, row, func(i int) tea.Cmd {
		if m.screen == on && i < len(l.matches) {
			l.cursor = i
		}
		return nil
	})
}

// logBarGuard prepares a click on the bar or the rows of the Logs screen: it is allowed where the keys reach
// them, and hands the focus to the screen first. It reports whether the click may go on, with the command the
// focus brought along.
func (m *Model) logBarGuard() (tea.Cmd, bool) {
	lv := m.logs
	if m.section != sectionLogs || lv.dialog != nil || lv.pick != nil || lv.list.editing {
		return nil, false
	}
	switch m.screen {
	case screenLogs:
		return nil, true
	case screenNav:
		cmd := m.focusList()
		return cmd, m.screen == screenLogs
	}
	return nil, false
}

// logBarTargets are the tabs, the fields of the filter line of the Logs screen, by their plain text: a click on a
// tab shows it, on the mode opens its list, on the date the dialog enter opens, on a filter the picker of its
// values, and on the search field starts the search, as the keys do.
func (m *Model) logBarTargets(mode, date string, tabs []string, filters []string, search string) []barSpec {
	lv := m.logs
	guarded := func(act func() tea.Cmd) func() tea.Cmd {
		return func() tea.Cmd {
			lead, ok := m.logBarGuard()
			if !ok {
				return lead
			}
			return tea.Batch(lead, act())
		}
	}
	var specs []barSpec
	for i, text := range tabs {
		specs = append(specs, barSpec{field: text, whole: guarded(func() tea.Cmd {
			lv.focus = logFocusTabs
			return m.setLogTab(logTab(i))
		})})
	}
	specs = append(specs, barSpec{field: mode, id: "logmode", whole: guarded(func() tea.Cmd { m.openLogBarDropdown(); return nil })})
	if lv.mode != logModeAll {
		specs = append(specs, barSpec{field: date, whole: guarded(func() tea.Cmd {
			lv.focus = logFocusDate
			m.openLogDateDialog()
			return nil
		})})
	}
	for k, text := range filters {
		specs = append(specs, barSpec{field: text, whole: guarded(func() tea.Cmd {
			m.openLogPick(k)
			return nil
		})})
	}
	if search != "" {
		specs = append(specs, barSpec{field: strings.TrimRight(search, " "), whole: guarded(func() tea.Cmd {
			lv.focus = logFocusRows
			lv.list.startFilter()
			return nil
		})})
	}
	return specs
}

// hitLogs makes the rows of the Logs screen clickable: a click takes the focus to the row and marks it, and a
// click on the marked row opens its entry, as enter does.
func (m *Model) hitLogs(head string, start, end int) {
	m.hitRows(m.hitTop(head), m.usable(0), start, end, func(int) string { return "" }, func(i int) tea.Cmd {
		lv := m.logs
		focused := m.screen == screenLogs && lv.focus == logFocusRows
		lead, ok := m.logBarGuard()
		if !ok || i >= len(lv.list.matches) {
			return lead
		}
		lv.focus = logFocusRows
		return tea.Batch(lead, markOrAct(&lv.list.cursor, i, focused, func() tea.Cmd { return m.press(tea.KeyPressMsg{Code: tea.KeyEnter}) }))
	})
}

// clickSection is a click on a section of a form, in the list of sections or on its heading: the focus goes to
// the first row of the section, as pgup and pgdown do.
func (m *Model) clickSection(name string) tea.Cmd {
	if m.pagePreview() {
		m.screen = screenForm
		m.clearMessages()
		m.applyFocus()
	}
	if !m.formClickable() {
		return nil
	}
	m.enterSection(name)
	m.syncProfile()
	return nil
}

// formClickable reports whether the rows of the form take clicks now: the form is the screen, and no question
// or key capture of it is open.
func (m *Model) formClickable() bool {
	return m.screen == screenForm && m.fields != nil && !m.payloadRemoveAsk && !m.payloadSaving && !m.keyCapture &&
		m.focus >= 0 && m.focus < len(m.fields)
}

// clickField is a click on a row of the form. It takes the focus to the row, which types nothing; on a row that
// opens a choice, a list or the masked prompt of a secret it also does what enter does. A row that acts at
// once (a vault action, the capture of a key) is only focused by the first click, and acts on the second.
func (m *Model) clickField(i int) tea.Cmd {
	if m.pagePreview() && i >= 0 && i < len(m.fields) && !m.fields[i].readOnly && !m.fields[i].hidden && !m.skipped(i) {
		// From the preview a click only moves the focus into the form, to the row it hit.
		m.screen = screenForm
		m.clearMessages()
		m.focus = i
		m.applyFocus()
		m.syncProfile()
		return nil
	}
	if !m.formClickable() || i < 0 || i >= len(m.fields) {
		return nil
	}
	f := m.fields[i]
	if f.readOnly || f.hidden || m.skipped(i) {
		return nil
	}
	focused := i == m.focus
	if !focused {
		m.trimFields()
		m.focus = i
		m.applyFocus()
		m.syncProfile()
	}
	switch f.kind {
	case fieldChoice, fieldMultiChoice, fieldProvider, fieldToolList, fieldTargets, fieldPaths, fieldSecret:
		return m.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	case fieldVaultAction, fieldKey:
		if focused {
			return m.press(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
	}
	return nil
}
