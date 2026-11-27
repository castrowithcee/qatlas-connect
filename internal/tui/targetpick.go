package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
)

// The target picker lets a person choose targets of a saved connection from the list its provider offers,
// instead of looking an ID up. It opens from the add menu of the target list and works like a file dialog:
// a table of the entries of one level with a search behind the search key, right opens the entry under the
// marker to the level below, left goes back up. Every entry of every level is a target of its own kind, so a
// higher level takes a whole area. What is picked is taken like a typed target (takeTarget), so the provider's check
// still decides. The provider's names and paths are untrusted text and are shown without control characters.
// Each level is a kit list whose entry IDs are indexes; the marks live across levels and tick the entries of
// the level shown.
//
// The core reads one page at a time (manage.Service.ListTargets); the picker reads off the event loop and
// asks for the next page when the marker reaches the last shown entry. Where the provider searches itself,
// the search text goes to it after a short pause; otherwise the kit filters the loaded entries.

// addPick is the row key prefix of the add menu that opens the picker on one top-level kind.
const addPick = "pick:"

// pickDelay is how long the search text rests before a searching provider is asked.
var pickDelay = 300 * time.Millisecond

// maxLabelRunes is the longest target label the configuration accepts.
const maxLabelRunes = 200

// pickLoadedMsg carries one page of a level, or the entries a URL resolved to, back into the event loop.
// replace says that it starts the level's entries over (first page, or another search text); cursor is the
// page that was asked for, and retried says that the read was the repeat after an admin login.
type pickLoadedMsg struct {
	req     int
	replace bool
	cursor  string
	retried bool
	page    config.TargetPage
	err     error
}

// pickSearchMsg is the pause after the last key of a search text of a level.
type pickSearchMsg struct{ level, seq int }

type pickEntry struct {
	entry config.TargetEntry
	// cells are the name, kind, ID and path as shown, already without control characters.
	cells []string
}

// pickLevel is one level of the picker: the entries of one kind under one parent.
type pickLevel struct {
	id int
	// kinds are the kinds that share this level's parent, ki the one shown.
	kinds  []string
	ki     int
	parent string
	// label is the parent as shown; empty on the top level.
	label string
	// entries are the loaded entries in the order they came; the index of one is its ID in list.
	entries []*pickEntry
	known   map[string]bool
	list    *kittui.List
	next    string
	// server says that the provider applies the search text itself; sent is the text its entries are for.
	server bool
	sent   string
	// req identifies the read in flight; a late answer of any other read is dropped.
	req     int
	loading bool
	cancel  context.CancelFunc
	err     string
	seq     int
}

func (lv *pickLevel) kind() string { return lv.kinds[lv.ki] }

// query is the search text of the level, without the blanks around it.
func (lv *pickLevel) query() string { return strings.TrimSpace(lv.list.Query()) }

// targetPick is the picker while it is open, over the add menu that opened it.
type targetPick struct {
	// connection names the saved connection that is read; without one, draft is read, the connection the
	// form or the setup is still building.
	connection string
	draft      *manage.ConnectionDraft
	// url, when set, is the web address the entries were resolved from: the picker then shows that one
	// answer and has no levels, pages or search of the provider.
	url       string
	levels    []*pickLevel
	nextID    int
	marks     map[string]config.TargetEntry
	markOrder []string
}

func (p *targetPick) top() *pickLevel { return p.levels[len(p.levels)-1] }

// pickSubject says what the picker reads for. A saved connection that the form still names as it was saved
// is read by name. Anything else, a new connection, a changed one or the guided setup, is read as the draft
// the form or the setup describes, since the core reads a saved connection as it was saved. ok is false
// where the provider has no way to list its targets or the draft names no service or credential yet.
func (m *Model) pickSubject() (connection string, draft *manage.ConnectionDraft, ok bool) {
	provider := m.formProvider()
	if provider == "" || !m.svc.CanBrowseTargets(provider) {
		return "", nil, false
	}
	if m.wizard != nil {
		if len(m.wizard.pages) <= stepCredential {
			return "", nil, false
		}
		d, _, _ := m.setupDraft(stepCredential)
		d.Targets = reachableOf(m.targetList.all, m.targetExcluded)
		return "", &d, d.Service != "" && d.Credential != ""
	}
	service, credential := m.formService(), m.fieldValue("credential")
	if saved, found := m.cfg.Connections[m.editing]; m.editing != "" && found &&
		saved.Service == service && saved.Credential == credential {
		return m.editing, nil, true
	}
	d := manage.ConnectionDraft{Provider: provider, Service: service, Credential: credential,
		Targets: reachableOf(m.targetList.all, m.targetExcluded)}
	return "", &d, service != "" && credential != ""
}

// pickRoots are the kinds the picker can start on: those without a parent.
func (m *Model) pickRoots() []string {
	if _, _, ok := m.pickSubject(); !ok {
		return nil
	}
	return m.pickKindsUnder("")
}

// pickKindsUnder lists the kinds shown below an entry of the kind parent, or at the top for "". A nested kind
// also sits below its own entries.
func (m *Model) pickKindsUnder(parent string) []string {
	var kinds []string
	for _, kind := range m.targetMetadata().Kinds {
		if kind.Unlisted {
			continue
		}
		if parent == "" && (len(kind.Parents) == 0 || kind.Top) || slices.Contains(kind.Parents, parent) || parent != "" && kind.Nested && kind.Name == parent {
			kinds = append(kinds, kind.Name)
		}
	}
	return kinds
}

func (m *Model) newPickLevel(p *targetPick, kinds []string, ki int, parent, label string) *pickLevel {
	p.nextID++
	lv := &pickLevel{id: p.nextID, kinds: kinds, ki: ki, parent: parent, label: label,
		known: map[string]bool{}, list: m.newPickList()}
	return lv
}

// newPickList is the kit list of a level: name, kind, ID and path, with a tick box where the provider takes
// several targets or they are taken as exclusions.
func (m *Model) newPickList() *kittui.List {
	list, err := kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{{Title: columnID("Name")},
		{Title: columnID("Kind"), Optional: true, Dimmed: true}, {Title: columnID("ID"), Dimmed: true},
		{Title: columnID("Path"), Optional: true, Dimmed: true}},
		Keys: listKeys(actSpace), Search: kittui.SearchByKey, Ticks: m.pickTicks(),
		Empty: tPickTheProviderListsNoEntry}, keymap, translator(false))
	if err != nil {
		panic("the list of the target picker is invalid: " + err.Error())
	}
	return list
}

// pickTicks reports whether the picker marks several entries.
func (m *Model) pickTicks() bool { return m.targetMetadata().Multiple || m.targetAsExclusion }

// startPick opens the picker on a top-level kind and starts reading its first page.
func (m *Model) startPick(kind string) tea.Cmd {
	connection, draft, ok := m.pickSubject()
	if !ok {
		m.fail = tr(tPickTheProviderSListCannot)
		return nil
	}
	roots := m.pickKindsUnder("")
	ki := 0
	for i, root := range roots {
		if root == kind {
			ki = i
		}
	}
	p := &targetPick{connection: connection, draft: draft, marks: map[string]config.TargetEntry{}}
	p.levels = []*pickLevel{m.newPickLevel(p, roots, ki, "", "")}
	m.targetPick = p
	m.clearMessages()
	return m.pickLoad(p.top(), "")
}

// closePick leaves the picker and abandons every read in flight.
func (m *Model) closePick() {
	if p := m.targetPick; p != nil {
		for _, lv := range p.levels {
			if lv.cancel != nil {
				lv.cancel()
			}
		}
	}
	m.targetPick = nil
}

// freshProof is the proof for the one read that repeats a refused one right after the person proved the
// admin passphrase. It is made from a single use that is spent at once, so it covers nothing else.
func freshProof() manage.AdminProof {
	session := manage.NewAdminSession(true, nil)
	session.Grant(0)
	proof, _ := session.Authorize(0)
	return proof
}

// pickLoad reads the page after cursor of a level, or its first page for an empty cursor. Starting a read
// abandons the one in flight.
func (m *Model) pickLoad(lv *pickLevel, cursor string) tea.Cmd {
	// The proof is taken here because the session is not safe for concurrent use. Without an active session
	// the core answers admin-required for an encrypted vault, which offers the login (see pickLoaded).
	proof, _ := m.admin.Authorize(m.adminTimeout())
	return m.pickRead(lv, cursor, proof, false)
}

// pickRead starts the read of a level, or of the URL of the picker, under proof.
func (m *Model) pickRead(lv *pickLevel, cursor string, proof manage.AdminProof, retried bool) tea.Cmd {
	if lv.cancel != nil {
		lv.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	lv.cancel = cancel
	m.pickSeq++
	lv.req, lv.loading, lv.err = m.pickSeq, true, ""
	p := m.targetPick
	svc, id := m.svc, lv.req
	if p.url != "" {
		connection, draft, rawURL := p.connection, p.draft, p.url
		return func() tea.Msg {
			var entries []config.TargetEntry
			var err error
			if draft != nil {
				entries, _, err = svc.ResolveDraftTargetURL(ctx, proof, *draft, rawURL)
			} else {
				entries, _, err = svc.ResolveTargetURL(ctx, proof, connection, rawURL)
			}
			return pickLoadedMsg{req: id, replace: true, retried: retried, page: config.TargetPage{Entries: entries},
				err: err}
		}
	}
	query := ""
	if lv.server {
		query = lv.query()
	}
	if cursor == "" {
		lv.sent = query
	}
	req := config.TargetListRequest{Kind: lv.kind(), Parent: lv.parent, Query: query, Cursor: cursor}
	connection, draft := p.connection, p.draft
	return func() tea.Msg {
		var page config.TargetPage
		var err error
		if draft != nil {
			page, _, err = svc.ListDraftTargets(ctx, proof, *draft, req)
		} else {
			page, _, err = svc.ListTargets(ctx, proof, connection, req)
		}
		return pickLoadedMsg{req: id, replace: req.Cursor == "", cursor: req.Cursor, retried: retried, page: page, err: err}
	}
}

// startResolve opens the picker on the entries a web address of the provider's service stands for. It
// reports whether the input was taken as an address to look up, which needs a way to list targets.
func (m *Model) startResolve(rawURL string) (tea.Cmd, bool) {
	connection, draft, ok := m.pickSubject()
	if !ok {
		return nil, false
	}
	p := &targetPick{connection: connection, draft: draft, url: rawURL, marks: map[string]config.TargetEntry{}}
	p.levels = []*pickLevel{m.newPickLevel(p, []string{"URL"}, 0, "", "")}
	m.targetPick = p
	m.clearMessages()
	return m.pickLoad(p.top(), ""), true
}

// looksLikeURL reports whether typed input is a web address, which is looked up rather than checked as a
// target.
func looksLikeURL(typed string) bool {
	lower := strings.ToLower(strings.TrimSpace(typed))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// pickLoaded takes the answer of a read, unless the level it was for is gone or has started another read.
func (m *Model) pickLoaded(msg pickLoadedMsg) tea.Cmd {
	p := m.targetPick
	if p == nil {
		return nil
	}
	var lv *pickLevel
	for _, level := range p.levels {
		if level.req == msg.req {
			lv = level
		}
	}
	if lv == nil || !lv.loading {
		return nil
	}
	lv.loading = false
	if lv.cancel != nil {
		lv.cancel()
		lv.cancel = nil
	}
	if msg.err != nil {
		lv.err = m.pickError(msg.err)
		var choice *manage.TargetChoiceError
		if errors.As(msg.err, &choice) && choice.Code == "admin-required" {
			if msg.retried || m.screen != screenTargets {
				return nil
			}
			// Like every managing action, the read asks for the admin passphrase and repeats itself once.
			return m.requireAdmin(func() tea.Cmd {
				if m.targetPick == nil || !slices.Contains(m.targetPick.levels, lv) {
					return nil
				}
				return m.pickRead(lv, msg.cursor, freshProof(), true)
			})
		}
		if p.url != "" {
			// The input stays typed in the add menu with the reason.
			m.closePick()
			m.fail = lv.err
		}
		return nil
	}
	lv.server = msg.page.ServerSearch
	if msg.replace {
		lv.entries, lv.known = nil, map[string]bool{}
	}
	for _, entry := range msg.page.Entries {
		if lv.known[entry.Target] {
			continue
		}
		lv.known[entry.Target] = true
		lv.entries = append(lv.entries, &pickEntry{entry: entry, cells: []string{m.pickText(entry.Name),
			m.pickText(entry.Kind), m.pickText(entry.Target), m.pickText(entry.Path)}})
	}
	lv.next = msg.page.Next
	m.pickShow(lv)
	if shown := lv.list.Shown(); msg.replace && len(shown) > 0 {
		lv.list.Select(shown[0])
	}
	if p.url != "" {
		return m.pickResolved(lv)
	}
	return m.pickSync(lv)
}

// pickShow hands the loaded entries of a level to its list and ticks the marked ones. The entries of a
// searching provider match the text they were read for, which keeps them shown until its next answer.
func (m *Model) pickShow(lv *pickLevel) {
	items := make([]kittui.ListItem, len(lv.entries))
	var ticked []kittui.ItemID
	for i, e := range lv.entries {
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: e.cells}
		if lv.server {
			items[i].SearchText = lv.sent
		}
		if _, marked := m.targetPick.marks[e.entry.Target]; marked {
			ticked = append(ticked, choiceID(i))
		}
	}
	if err := lv.list.SetItems(items); err != nil {
		panic("the entries of the target picker are invalid: " + err.Error())
	}
	if err := lv.list.SetTicked(ticked); err != nil {
		panic("the marks of the target picker are invalid: " + err.Error())
	}
}

// pickResolved acts on the entries a URL stood for: none keeps the typed input, one is taken like an entry
// of the list, several are left to choose from.
func (m *Model) pickResolved(lv *pickLevel) tea.Cmd {
	var found []config.TargetEntry
	for _, e := range lv.entries {
		found = append(found, e.entry)
	}
	switch len(found) {
	case 0:
		m.closePick()
		m.fail = tr(tPickTheProviderFoundNoEntry,
			m.targetMetadata().Label)
	case 1:
		if !m.takeEntries(found) {
			m.closePick()
		}
	}
	return nil
}

// pickError is the failure of a read as the core states it: its code and its fixed text.
func (m *Model) pickError(err error) string {
	var choice *manage.TargetChoiceError
	if errors.As(err, &choice) {
		return choice.Code + ": " + choice.Error()
	}
	return tr(tNoteError, m.pickText(m.redactor.Apply(err.Error())))
}

// pickSync asks a searching provider for the search text as it stands now, once the level shows the entries
// of another text.
func (m *Model) pickSync(lv *pickLevel) tea.Cmd {
	if lv != m.targetPick.top() || !lv.server || lv.query() == lv.sent {
		return nil
	}
	return m.pickLoad(lv, "")
}

// pickSearch is the end of the pause after a search text.
func (m *Model) pickSearch(msg pickSearchMsg) tea.Cmd {
	if m.targetPick == nil {
		return nil
	}
	lv := m.targetPick.top()
	if lv.id != msg.level || lv.seq != msg.seq {
		return nil
	}
	return m.pickSync(lv)
}

// pickMore reads the next page once the marker is on the last shown entry, or nothing shows.
func (m *Model) pickMore(lv *pickLevel) tea.Cmd {
	if lv.next == "" || lv.loading {
		return nil
	}
	if shown := lv.list.Shown(); len(shown) > 0 {
		if id, ok := lv.list.Selected(); !ok || id != shown[len(shown)-1] {
			return nil
		}
	}
	return m.pickLoad(lv, lv.next)
}

// pickText makes untrusted text of a provider safe to print.
func (m *Model) pickText(s string) string { return m.logText(s) }

// pickLabel is the name of an entry as a target label: one line without control or invisible formatting
// characters, no longer than the configuration accepts.
func (m *Model) pickLabel(name string) string {
	s := strings.ToValidUTF8(m.redactor.Apply(name), "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxLabelRunes {
		s = strings.TrimSpace(string([]rune(s)[:maxLabelRunes]))
	}
	return s
}

// updatePick handles the picker. The level's list takes its keys first: it moves, ticks and, after the
// search key, types the search, and esc clears a search before it goes a level up. enter takes the marks or
// the entry under the marker, right opens that entry and left goes back up.
func (m *Model) updatePick(key tea.KeyPressMsg) tea.Cmd {
	p := m.targetPick
	lv := p.top()
	m.clearMessages()
	switch {
	case keyIs(key, actEnter) && !lv.list.Searching():
		m.pickTake(true)
		return nil
	case keyIs(key, actNextKind):
		return m.pickNextKind(lv)
	}
	before := lv.query()
	if content, ok := pasted(key); ok {
		if lv.list.Paste(content) {
			return m.pickMoved(lv, before)
		}
		return nil
	}
	if _, handled := lv.list.Update(key); handled {
		return m.pickMoved(lv, before)
	}
	switch {
	case keyIs(key, actBack):
		switch {
		case len(p.levels) > 1:
			return m.pickBack()
		case len(p.marks) > 0:
			m.askLeave(false)
		default:
			m.closePick()
		}
	case keyIs(key, actRight):
		return m.pickOpen(lv)
	case keyIs(key, actLeft):
		return m.pickBack()
	case keyIs(key, actSpace):
		// The list has no tick boxes: the provider takes one target only.
		if _, ok := lv.current(); ok {
			m.fail = tr(tPickThisProviderTakesOneOnly, m.targetMetadata().Label)
		}
	case keyIs(key, actReload):
		return m.pickLoad(lv, "")
	}
	return nil
}

// pickMoved follows a key or a click the level's list took: the ticks become marks, a changed search goes
// to a searching provider after a pause, and the marker on the last shown entry reads the next page.
func (m *Model) pickMoved(lv *pickLevel, before string) tea.Cmd {
	m.pickTicked(lv)
	if lv.server && lv.query() != before {
		lv.seq++
		level, seq := lv.id, lv.seq
		return tea.Tick(pickDelay, func(time.Time) tea.Msg { return pickSearchMsg{level: level, seq: seq} })
	}
	return m.pickMore(lv)
}

// pickTicked takes the ticks of the level's entries into the marks, which hold the ticks of every level.
func (m *Model) pickTicked(lv *pickLevel) {
	p := m.targetPick
	ticked := map[kittui.ItemID]bool{}
	for _, id := range lv.list.Ticked() {
		ticked[id] = true
	}
	for i, e := range lv.entries {
		target := e.entry.Target
		_, marked := p.marks[target]
		switch on := ticked[choiceID(i)]; {
		case on && !marked:
			p.marks[target] = e.entry
			p.markOrder = append(p.markOrder, target)
		case !on && marked:
			delete(p.marks, target)
			p.markOrder = slices.DeleteFunc(p.markOrder, func(t string) bool { return t == target })
		}
	}
}

// current is the entry under the marker.
func (lv *pickLevel) current() (*pickEntry, bool) {
	i, ok := chosen(lv.list, len(lv.entries))
	if !ok {
		return nil, false
	}
	return lv.entries[i], true
}

// pickOpen opens the entry under the marker to the level below it.
func (m *Model) pickOpen(lv *pickLevel) tea.Cmd {
	e, ok := lv.current()
	if !ok {
		return nil
	}
	kinds := m.pickKindsUnder(lv.kind())
	if !e.entry.HasChildren || len(kinds) == 0 || m.targetPick.url != "" {
		m.status = tr(tPickNothingLiesBelowThisEntry)
		return nil
	}
	if lv.cancel != nil {
		// The page that was coming is read again if the person returns and reaches the end.
		lv.cancel()
		lv.cancel, lv.loading = nil, false
	}
	p := m.targetPick
	label := e.cells[2]
	if e.cells[0] != "" && e.cells[0] != label {
		label = e.cells[0] + " (" + label + ")"
	}
	child := m.newPickLevel(p, kinds, 0, e.entry.Target, label)
	p.levels = append(p.levels, child)
	return m.pickLoad(child, "")
}

// pickBack goes one level up, dropping the level it leaves.
func (m *Model) pickBack() tea.Cmd {
	p := m.targetPick
	if len(p.levels) == 1 {
		m.status = tr(tPickAlreadyOnTheTopLevel)
		return nil
	}
	left := p.top()
	if left.cancel != nil {
		left.cancel()
	}
	p.levels = p.levels[:len(p.levels)-1]
	return m.pickSync(p.top())
}

// pickNextKind shows the next kind of the level's siblings, which lists other entries under the same parent.
func (m *Model) pickNextKind(lv *pickLevel) tea.Cmd {
	if len(lv.kinds) < 2 {
		return nil
	}
	lv.ki = (lv.ki + 1) % len(lv.kinds)
	lv.entries, lv.known, lv.list = nil, map[string]bool{}, m.newPickList()
	lv.next, lv.server, lv.sent = "", false, ""
	return m.pickLoad(lv, "")
}

// pickTake takes the marked entries, or the entry under the marker when none is marked and useCursor is
// set, into the list like typed targets. Nothing is taken unless every one is acceptable. A label is set
// from the provider's name only where the target has none. It reports whether the picker is done.
func (m *Model) pickTake(useCursor bool) bool {
	p := m.targetPick
	var picked []config.TargetEntry
	for _, target := range p.markOrder {
		picked = append(picked, p.marks[target])
	}
	if len(picked) == 0 && useCursor {
		if e, ok := p.top().current(); ok {
			picked = append(picked, e.entry)
		}
	}
	return m.takeEntries(picked)
}

// takeEntries puts entries into the list like typed targets, all or none, and closes the picker when it did.
func (m *Model) takeEntries(picked []config.TargetEntry) bool {
	if len(picked) == 0 {
		return false
	}
	pending := map[string]bool{}
	if !m.targetAsExclusion && m.tooManyTargets(len(picked)) {
		m.fail = tr(tPickThisProviderTakesOneOnly, m.targetMetadata().Label)
		return false
	}
	for _, e := range picked {
		problem := m.targetProblem(e.Target, len(m.targetList.all))
		if problem == "" && pending[e.Target] {
			problem = tr(tPickThisIsAlreadyInThe, m.targetMetadata().Label)
		}
		if problem == "" && m.targetAsExclusion && !m.canExclude(e.Target) {
			problem = tr(tTargetCannotBeExcluded, m.targetMetadata().Label)
		}
		if problem != "" {
			m.fail = m.pickText(e.Target) + ": " + problem
			return false
		}
		pending[e.Target] = true
	}
	for _, e := range picked {
		m.editTarget(len(m.targetList.all), e.Target)
		m.takeTarget()
		if label := m.pickLabel(e.Name); label != "" && m.targetLabels[e.Target] == "" {
			if m.targetLabels == nil {
				m.targetLabels = map[string]string{}
			}
			m.targetLabels[e.Target] = label
		}
	}
	m.closePick()
	m.targetAdd = nil
	m.clearMessages()
	m.status = tr(tPickAddedFromTheProvider, len(picked))
	return true
}

// position is where the marker stands among the entries, as the title shows it; a "+" says more pages follow.
func (lv *pickLevel) position() string {
	pos := lv.list.Position()
	if pos == "" {
		pos = "0"
	}
	if lv.next != "" {
		pos += "+"
	}
	return pos
}

// pickNote sets the sentences of the level's list: its state while it loads or failed, and where no loaded
// entry matches but more pages follow.
func (m *Model) pickNote(lv *pickLevel) {
	empty, note, warn := tr(tPickTheProviderListsNoEntry), "", false
	switch {
	case lv.err != "":
		empty, note, warn = tr(tPickReloadReadsAgain), lv.err, true
		if len(lv.entries) > 0 {
			note += " " + tr(tPickReloadReadsAgain)
		}
	case lv.loading:
		empty = tr(tLogLoading)
		if len(lv.entries) > 0 {
			note = tr(tLogLoading)
		}
	case len(lv.entries) > 0 && lv.next != "" && len(lv.list.Shown()) == 0:
		note = tr(tPickNoLoadedEntryMatchesDown, lv.query())
	}
	lv.list.SetEmpty(empty)
	lv.list.SetNote(note, warn)
}

// pickFrame is what stands above and below the list of the level.
func (m *Model) pickFrame() (string, string) {
	p := m.targetPick
	lv := p.top()
	panel := m.dialogPanelOf()
	title := tr(tPickChooseFromTheList, lv.kind(), lv.position())
	if p.url != "" {
		title = tr(tPickEntriesFoundForTheAddress, lv.position())
	}
	if len(p.marks) > 0 {
		title += tr(tPickMarked, len(p.marks))
	}
	if m.targetAsExclusion {
		title += " · " + tr(tTargetTakenAsExclusionShort)
	}
	var where []string
	for i, level := range p.levels {
		if i > 0 {
			where = append(where, level.label)
		}
		where = append(where, level.kind())
	}
	level := tr(tPickLevel, strings.Join(where, " › "))
	if p.url != "" {
		level = tr(tPickTheAddressYouTypedMatched)
	}
	head := panel.top(title, "") + panel.text(styles.hint, level) + panel.blank()
	return head, panel.closing() + m.keyHint(m.pickKeys()) + m.notes()
}

// pickKeys is the key text of the target picker, for the level it stands on; while a search is typed, the
// list's own.
func (m *Model) pickKeys() string {
	p := m.targetPick
	lv := p.top()
	if lv.list.Searching() {
		return hintsText(lv.list.Hints())
	}
	keys := "{up}/{down} move · {enter} take"
	if _, ok := lv.current(); ok && p.url == "" && len(m.pickKindsUnder(lv.kind())) > 0 {
		keys += " · {right} open"
	}
	if m.pickTicks() {
		keys += " · {space} tick"
	}
	keys += " · {search} search"
	if len(lv.kinds) > 1 {
		keys += " · {nextkind} next kind"
	}
	switch {
	case lv.query() != "":
		keys += " · {save} save · {back} clear filter"
	case len(p.levels) > 1:
		keys += " · {save} save · {back} level up"
	default:
		keys += " · {save} save · {back} close"
	}
	return keys + " · {command} cmds · {help} help"
}

// pickView draws the picker: its frame around the list of the level shown.
func (m *Model) pickView() string {
	lv := m.targetPick.top()
	m.pickNote(lv)
	head, foot := m.pickFrame()
	return m.workList(lv.list, head, foot)
}
