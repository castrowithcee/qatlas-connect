// The request part of the Approvals section: the tool requests agents left in the queue beside the
// configuration, shown whatever the state of the vault, and their decision by a person. What a decision
// does, and every check before it, is the core's (manage.PreviewRequestDecision, manage.DecideRequest);
// nothing here decides a rule.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
)

// requestRowPrefix starts the list name of a request, so it never clashes with a connection name.
const requestRowPrefix = "\x01req:"

// requestRowName is the name a request has in the Approvals list.
func requestRowName(id string) string { return requestRowPrefix + id }

// requestIDOf is the request a list name stands for.
func requestIDOf(name string) (string, bool) { return strings.CutPrefix(name, requestRowPrefix) }

// removes reports whether a request asks to remove tools.
func removes(r manage.OpenRequest) bool { return r.Kind == accessrequest.KindRemoveTools }

// requestChoice is the open choice of how to accept on a connection that offers its tools by effect.
type requestChoice struct {
	selection manage.RequestSelection
	preview   manage.RequestPreview
	modes     []manage.AcceptMode
	cursor    int
}

// requestRow is one line of the tools of the detail: a group heading or a tool entry.
type requestRow struct {
	heading string
	count   int
	entry   string
}

// refreshRequests reads the queue again. It runs when the Overview or Approvals open, with the periodic
// refresh of the header, and after every decision of this editor, because agents add to the queue at any time.
func (m *Model) refreshRequests() {
	requests, err := m.svc.OpenRequests()
	m.requestsErr = ""
	if err != nil {
		m.requestsErr = m.redactor.Apply(err.Error())
		requests = nil
	}
	m.requests = requests
	if m.requestDetail != "" {
		m.syncRequestTicks()
	}
}

// syncRequestTicks drops the ticks of entries that left the open request; an entry that joined by a new
// request starts ticked, like on opening.
func (m *Model) syncRequestTicks() {
	req, ok := m.requestByID(m.requestDetail)
	if !ok {
		return
	}
	now := map[string]bool{}
	for _, e := range req.Entries {
		if ticked, known := m.requestTicks[e.Tool]; known {
			now[e.Tool] = ticked
		} else {
			now[e.Tool] = true
		}
	}
	m.requestTicks = now
	m.requestCursor = min(m.requestCursor, max(len(m.requestRows(req))-1, 0))
}

// syncRequestList rebuilds the Approvals list after a refresh, keeping the selected row.
func (m *Model) syncRequestList() {
	if m.section != sectionApprovals || m.screen != screenList || m.list.editing {
		return
	}
	name, _ := m.list.selected()
	m.list.setItems(m.listItems(m.section))
	if name != "" {
		m.list.selectName(name)
	}
}

func (m *Model) requestByID(id string) (manage.OpenRequest, bool) {
	for _, r := range m.requests {
		if r.ID == id {
			return r, true
		}
	}
	return manage.OpenRequest{}, false
}

// requestRowSelected reports whether the cursor of the Approvals list stands on a request.
func (m *Model) requestRowSelected() bool {
	if m.section != sectionApprovals {
		return false
	}
	name, ok := m.selected()
	if !ok {
		return false
	}
	_, isRequest := requestIDOf(name)
	return isRequest
}

// requestNames are the list names of the open requests, newest first.
func (m *Model) requestNames() []string {
	names := make([]string, len(m.requests))
	for i, r := range m.requests {
		names[i] = requestRowName(r.ID)
	}
	return names
}

// requestExpiry is the time left of a request in the coarsest unit that is not zero.
func (m *Model) requestExpiry(r manage.OpenRequest) string {
	// Rounded up, so a request filed a moment ago reads as its full period.
	left := r.ExpiresAt.Sub(m.timeNow())
	switch {
	case left >= 48*time.Hour:
		return tr(tRequestExpiresDays, int((left+24*time.Hour-1)/(24*time.Hour)))
	case left > time.Hour:
		return tr(tRequestExpiresHours, int((left+time.Hour-1)/time.Hour))
	}
	return tr(tRequestExpiresSoon)
}

// requestCells are the columns of a request row: connection, kind, and what the request holds.
func (m *Model) requestCells(id string) []string {
	r, ok := m.requestByID(id)
	if !ok {
		return []string{"", tr(tRequestKind), ""}
	}
	parts := []string{tr(tRequestCellTools, len(r.Entries)), m.requestExpiry(r)}
	if r.Project != "" {
		parts = append(parts, tr(tRequestCellProject, m.logText(r.Project)))
	}
	kind := tr(tRequestKind)
	if removes(r) {
		kind = tr(tRequestKindRemove)
	}
	return []string{m.logText(r.Connection), kind, strings.Join(parts, " · ")}
}

// requestRows lays the entries of a request out as the tool picker groups them: a heading per group and the
// entries under it. A provider without groups has no headings.
func (m *Model) requestRows(r manage.OpenRequest) []requestRow {
	var rows []requestRow
	last, grouped := "", false
	for _, e := range r.Entries {
		if e.Group != "" {
			grouped = true
		}
	}
	for i, e := range r.Entries {
		if grouped && (i == 0 || e.Group != last) {
			title, count := e.GroupTitle, 0
			if e.Group == "" {
				title = tr(tToolGroupNotRegistered)
			}
			for _, o := range r.Entries {
				if o.Group == e.Group {
					count++
				}
			}
			rows = append(rows, requestRow{heading: title, count: count})
		}
		last = e.Group
		rows = append(rows, requestRow{entry: e.Tool})
	}
	return rows
}

// ticked are the entries of the request that are ticked now, in the order of the request.
func (m *Model) tickedEntries(r manage.OpenRequest) []string {
	var out []string
	for _, e := range r.Entries {
		if m.requestTicks[e.Tool] {
			out = append(out, e.Tool)
		}
	}
	return out
}

// openRequest opens the detail of a request with every tool ticked.
func (m *Model) openRequest(id string) {
	m.requestDetail, m.requestCursor, m.requestTop, m.requestChoice = id, 0, 0, nil
	m.requestTicks = map[string]bool{}
	if r, ok := m.requestByID(id); ok {
		for _, e := range r.Entries {
			m.requestTicks[e.Tool] = true
		}
	}
	m.screen = screenConfirm
	m.clearMessages()
}

// closeRequest leaves the detail for the list.
func (m *Model) closeRequest() {
	m.requestDetail, m.requestChoice, m.requestTicks = "", nil, nil
	m.screen = screenList
}

// toggleRequestRow ticks or unticks the tool under the cursor, or all tools of the group under a heading.
func (m *Model) toggleRequestRow(r manage.OpenRequest) {
	rows := m.requestRows(r)
	if m.requestCursor < 0 || m.requestCursor >= len(rows) {
		return
	}
	row := rows[m.requestCursor]
	if row.heading == "" {
		m.requestTicks[row.entry] = !m.requestTicks[row.entry]
		return
	}
	var members []string
	for i := m.requestCursor + 1; i < len(rows) && rows[i].heading == ""; i++ {
		members = append(members, rows[i].entry)
	}
	all := true
	for _, e := range members {
		all = all && m.requestTicks[e]
	}
	for _, e := range members {
		m.requestTicks[e] = !all
	}
}

// updateRequestDetail handles a key on the detail of a request or on its choice.
func (m *Model) updateRequestDetail(key tea.KeyPressMsg) tea.Cmd {
	r, ok := m.requestByID(m.requestDetail)
	if !ok {
		if keyIs(key, actBack) || keyIs(key, actEnter) {
			m.closeRequest()
			return m.returnToList("")
		}
		return nil
	}
	if c := m.requestChoice; c != nil {
		switch {
		case keyIs(key, actBack):
			m.requestChoice = nil
		case keyIs(key, actUp):
			c.cursor = max(c.cursor-1, 0)
		case keyIs(key, actDown):
			c.cursor = min(c.cursor+1, len(c.modes)-1)
		case keyIs(key, actEnter), keyIs(key, actYes):
			return m.decideRequest(manage.RequestDecision{RequestSelection: c.selection, Action: manage.RequestAccept, Mode: c.modes[c.cursor]})
		}
		return nil
	}
	rows := m.requestRows(r)
	switch {
	case keyIs(key, actBack):
		m.closeRequest()
		return m.returnToList(requestRowName(r.ID))
	case keyIs(key, actUp) || keyIs(key, actVimUp):
		m.requestCursor = max(m.requestCursor-1, 0)
	case keyIs(key, actDown) || keyIs(key, actVimDown):
		m.requestCursor = min(m.requestCursor+1, max(len(rows)-1, 0))
	case keyIs(key, actHome):
		m.requestCursor = 0
	case keyIs(key, actEnd):
		m.requestCursor = max(len(rows)-1, 0)
	case keyIs(key, actPageUp):
		m.requestCursor = max(m.requestCursor-max(m.requestPage, 1), 0)
	case keyIs(key, actPageDown):
		m.requestCursor = min(m.requestCursor+max(m.requestPage, 1), max(len(rows)-1, 0))
	case keyIs(key, actSpace):
		m.toggleRequestRow(r)
	case keyIs(key, actTickAll):
		all := len(m.tickedEntries(r)) == len(r.Entries)
		for _, e := range r.Entries {
			m.requestTicks[e.Tool] = !all
		}
	case keyIs(key, actYes):
		return m.acceptRequest(r)
	case keyIs(key, actDecline):
		return m.decideTicked(r, manage.RequestDecline)
	case keyIs(key, actBlock) && !removes(r):
		return m.decideTicked(r, manage.RequestBlock)
	}
	return nil
}

// selectionOf is the selection of the ticked entries, or false after saying that none is ticked.
func (m *Model) selectionOf(r manage.OpenRequest) (manage.RequestSelection, bool) {
	ticked := m.tickedEntries(r)
	if len(ticked) == 0 {
		m.status = tr(tRequestNoneTicked)
		return manage.RequestSelection{}, false
	}
	return manage.RequestSelection{Request: r.ID, Tools: ticked}, true
}

// decideTicked declines or blocks the ticked tools.
func (m *Model) decideTicked(r manage.OpenRequest, action manage.RequestAction) tea.Cmd {
	sel, ok := m.selectionOf(r)
	if !ok {
		return nil
	}
	return m.decideRequest(manage.RequestDecision{RequestSelection: sel, Action: action})
}

// acceptRequest accepts the ticked tools. A connection that offers its tools by effect first asks how, with
// the consequence of each way as the core's preview names it.
func (m *Model) acceptRequest(r manage.OpenRequest) tea.Cmd {
	sel, ok := m.selectionOf(r)
	if !ok {
		return nil
	}
	preview, err := m.svc.PreviewRequestDecision(sel)
	if err != nil {
		m.fail = m.redactor.Apply(err.Error())
		m.refreshRequests()
		return nil
	}
	if removes(r) {
		m.openRemoveChoice(sel, preview)
		return nil
	}
	if !preview.ByEffects || len(preview.Entries) == 0 {
		return m.decideRequest(manage.RequestDecision{RequestSelection: sel, Action: manage.RequestAccept})
	}
	c := &requestChoice{selection: sel, preview: preview}
	if len(preview.AddEffectsBlockedBy) == 0 {
		c.modes = append(c.modes, manage.AcceptAddEffects)
	}
	c.modes = append(c.modes, manage.AcceptSwitchToList)
	m.requestChoice = c
	m.clearMessages()
	return nil
}

// requestDecisionMsg carries the outcome of a decision back into the event loop.
type requestDecisionMsg struct {
	result manage.RequestDecisionResult
	err    error
}

// decideRequest decides through the admin session like every managing action. The core's check of the
// admin proof stays the authority; this only asks for the passphrase when an encrypted vault needs one.
func (m *Model) decideRequest(in manage.RequestDecision) tea.Cmd {
	return m.requireAdmin(func() tea.Cmd {
		if m.vaultBusy {
			m.status = tr(tVaultWriteBusy)
			return nil
		}
		proof, _ := m.admin.Authorize(m.adminTimeout())
		svc := m.svc
		m.vaultBusy = true
		m.writes++
		m.busy = tr(tBusyDecidingRequest)
		return func() tea.Msg {
			res, err := svc.DecideRequest(context.Background(), proof, in)
			return requestDecisionMsg{result: res, err: err}
		}
	})
}

// handleRequestDecision shows the outcome: the list again with the decision in the status line, or the
// reason it was refused on the screen it was taken from.
func (m *Model) handleRequestDecision(msg requestDecisionMsg) tea.Cmd {
	m.vaultBusy = false
	if m.writes > 0 {
		m.writes--
	}
	if m.writes == 0 {
		m.busy = ""
	}
	if msg.err != nil {
		m.requestChoice = nil
		if m.conflicted(msg.err) {
			m.refreshRequests()
			return nil
		}
		var refused *manage.RequestDecisionError
		if errors.As(msg.err, &refused) && refused.Code == manage.RequestCodeNotFound {
			m.fail = m.redactor.Apply(refused.Message)
		} else {
			m.fail = m.redactor.Apply(msg.err.Error())
		}
		m.refreshRequests()
		return nil
	}
	res := msg.result
	removed := false
	if r, ok := m.requestByID(m.requestDetail); ok {
		removed = removes(r)
	}
	if err := m.reloadConfig(); err != nil {
		m.fail = m.redactor.Apply(err.Error())
	}
	m.refreshRequests()
	m.closeRequest()
	cmd := m.returnToList("")
	status := ""
	switch res.Action {
	case manage.RequestAccept:
		status = tr(tRequestAccepted, len(res.Decided), res.Connection)
		if removed {
			status = tr(tRequestRemoved, len(res.Decided), res.Connection)
		}
	case manage.RequestDecline:
		status = tr(tRequestDeclined, len(res.Decided), res.Connection)
	case manage.RequestBlock:
		status = tr(tRequestBlockedDone, len(res.Decided), res.Connection)
	}
	if n := len(res.Dropped); n > 0 {
		status += tr(tRequestDroppedNote, n)
	}
	if n := len(res.Remaining); n > 0 {
		status += tr(tRequestStayOpen, n)
	}
	switch res.Approval.State {
	case manage.ApprovalGranted:
		status += tr(tRequestApproved)
	case manage.ApprovalStaysOpen:
		status += tr(tRequestApprovalOpen, res.Connection)
	}
	for _, w := range res.Warnings {
		status += tr(tNoteWarning, m.redactor.Apply(w))
	}
	m.status = status
	return cmd
}

// requestDetailView draws the detail of a request or its choice.
func (m *Model) requestDetailView() string {
	r, ok := m.requestByID(m.requestDetail)
	if !ok {
		return m.wrapped(styles.title, tr(tRequestTitle, "")) + "\n\n" + m.wrapped(styles.hint, tr(tRequestClosed)) +
			m.keyHint(keysBackOnly)
	}
	if m.requestChoice != nil {
		return m.requestChoiceView(r)
	}
	return m.requestBodyView(r)
}

// requestFacts are the rows above the tools: state, connection, project, times, and the reason, which is
// the agent's text and says so.
func (m *Model) requestFacts(r manage.OpenRequest) []styledFact {
	facts := m.statusFact(statusMark(statusWarn, tr(tStatusOpen)))
	add := func(label, value string) {
		facts = append(facts, styledFact{detailFact: detailFact{label: label, value: value}})
	}
	add(tr(tRequestLblConnection), m.logText(r.Connection))
	if r.Project != "" {
		add(tr(tRequestLblProject), m.logText(r.Project))
	}
	add(tr(tRequestLblAsked), r.RequestedAt.In(time.Local).Format("2006-01-02 15:04"))
	add(tr(tRequestLblExpires), r.ExpiresAt.In(time.Local).Format("2006-01-02 15:04"))
	reason := tr(tRequestNoReason)
	if r.Reason != "" {
		reason = `"` + m.redactor.Apply(r.Reason) + `"`
	}
	add(tr(tRequestLblReason), reason)
	if r.Reason != "" {
		facts = append(facts, styledFact{detailFact: detailFact{value: tr(tRequestReasonNote)}, style: styles.warning})
	}
	return facts
}

// requestBodyView is the detail: the facts, then the tools in their groups with a tick each.
func (m *Model) requestBodyView(r manage.OpenRequest) string {
	inner, _ := m.detailInner()
	facts := m.requestFacts(r)
	lines := factLinesIn(facts, inner, factColumn(facts, inner), func(s string) string { return s })
	lines = append(lines, "", styles.title.Render(tr(tRequestToolsHeading, len(m.tickedEntries(r)), len(r.Entries))))
	first := len(lines)
	for i, row := range m.requestRows(r) {
		cursor := "  "
		if i == m.requestCursor {
			cursor = "> "
		}
		switch {
		case row.heading != "":
			lines = append(lines, cursor+styles.active.Render(row.heading)+styles.hint.Render(fmt.Sprintf(" (%d)", row.count)))
		default:
			lines = append(lines, "  "+cursor+tickBox(m.requestTicks[row.entry])+" "+m.toolEntryLabel(r.Provider, row.entry))
		}
	}
	keys := keysRequestBody
	if removes(r) {
		keys = keysRequestRemoveBody
	}
	room := m.height - m.detailChrome() - strings.Count(m.keyHint(keys), "\n") - 2
	room = max(room, 1)
	m.requestPage = room
	// The cursor stays in the window; on the first row the facts above the tools show as well.
	at := first + m.requestCursor
	switch {
	case m.requestCursor == 0:
		m.requestTop = 0
	case at < m.requestTop:
		m.requestTop = at
	case at >= m.requestTop+room:
		m.requestTop = at - room + 1
	}
	m.requestTop = min(max(m.requestTop, 0), max(len(lines)-room, 0))
	end := min(m.requestTop+room, len(lines))
	title := tr(tRequestTitle, m.logText(r.Connection))
	if removes(r) {
		title = tr(tRequestTitleRemove, m.logText(r.Connection))
	}
	return m.frameDetail(title, statusMark(statusWarn, tr(tStatusOpen)), lines[m.requestTop:end]) +
		"\n" + m.keyHint(keys)
}

// requestChoiceView is the choice of how to accept on a connection by effect, each way with what it does.
func (m *Model) requestChoiceView(r manage.OpenRequest) string {
	c := m.requestChoice
	if removes(r) {
		return m.requestRemoveChoiceView(r)
	}
	var details []string
	for i, mode := range c.modes {
		cursor := "  "
		if i == c.cursor {
			cursor = "> "
		}
		title := tr(tRequestOptSwitch)
		var follow []string
		if mode == manage.AcceptAddEffects {
			title = tr(tRequestOptAdd)
			for _, g := range c.preview.AddEffects {
				follow = append(follow, tr(tRequestAddGain, string(g.Effect), g.Tools))
			}
		} else {
			follow = append(follow, tr(tRequestSwitchFollow, c.preview.SwitchTools))
		}
		details = append(details, m.indented(cursor+"("+radioMark(i == c.cursor)+") "+title))
		for _, line := range follow {
			details = append(details, m.indentedWith(styles.hint, "      "+line))
		}
	}
	if len(c.preview.AddEffectsBlockedBy) > 0 {
		details = append(details, m.indentedWith(styles.warning, tr(tRequestAddBlocked, m.logText(c.preview.AddEffectsBlockedBy[0]))))
	}
	if c.preview.ApprovalExpected {
		details = append(details, m.indentedWith(styles.hint, tr(tRequestApprovalExpected)))
	}
	return m.questionText(question{
		title:       tr(tRequestChoiceTitle, m.logText(r.Connection)),
		consequence: tr(tRequestChoiceConsequence),
		details:     details,
		keys:        keysRequestChoice,
	})
}

// radioMark is the mark of a chosen or other option.
func radioMark(chosen bool) string {
	if chosen {
		return "*"
	}
	return " "
}

// requestsNote is the note under the Approvals list when the part of the connection approvals has nothing to
// list while requests are shown: why the vault cannot list them, or why the queue cannot be read. total is
// the number of rows.
func (m *Model) requestsNote(total int) string {
	var note string
	if m.requestsErr != "" {
		note = m.hint(tr(tRequestsReadError, m.requestsErr))
	}
	if _, unavailable := m.approvalsReport(); unavailable != "" && total > 0 {
		note += m.hint(tr(tRequestsApprovalsNote, unavailable))
	}
	return note
}
