package tui

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// Reusable entries can be duplicated with p on a list, and chosen as the template of a new entry with n,
// and the guided setup can start from a configured connection. Copying means filling the ordinary form of a
// new entry: nothing is saved before the form is, and saving stays the managing action every form is (see
// submit). Secret values are never part of an entry, so none is ever copied; a credential copied from a
// keyring or vault credential keeps only where its secrets are kept.

// templateChoice is the state of screenTemplate: the question first, then the list of the entries.
type templateChoice struct {
	// from is the screen to return to when the question is cancelled or an admin dialog is cancelled.
	from screen
	// setup is true when the question belongs to the guided setup instead of the active section.
	setup bool
	// picking is true once "from existing" was chosen and the entries are searched.
	picking bool
	// kind is true while the question is which kind of credential to create (see askKind); the template
	// question of a credential follows an answer of credential.
	kind bool
	// list is the kit's list of the step shown: the question, or the entries, which names holds by the
	// index that is the entry ID.
	list  *kittui.List
	names []string
}

// templateSection reports whether a section holds reusable entries that can be copied.
func (m *Model) templateSection(s section) bool {
	switch s {
	case sectionServices, sectionCredentials, sectionConnections, sectionTokens:
		return true
	}
	return false
}

// templateNames are the entries the picker offers: those of the section, or the connections for the setup.
func (m *Model) templateNames(setup bool) []string {
	if setup {
		return m.entryNames(sectionConnections)
	}
	return m.entryNames(m.section)
}

// templateOffered reports whether n asks for a template first. Without an entry to copy there is nothing
// to ask.
func (m *Model) templateOffered() bool {
	return m.templateSection(m.section) && len(m.templateNames(false)) > 0
}

// askTemplate opens the template question of the active section, or of the guided setup.
func (m *Model) askTemplate(setup bool) {
	m.openTemplate(&templateChoice{from: m.screen, setup: setup})
}

// askKind opens the question behind n in Credentials: a credential or a payload secret.
func (m *Model) askKind() {
	m.openTemplate(&templateChoice{from: m.screen, kind: true})
}

func (m *Model) openTemplate(t *templateChoice) {
	m.clearMessages()
	if err := m.templateQuestion(t, 0); err != nil {
		m.fail = err.Error()
		return
	}
	m.template = t
	m.screen = screenTemplate
	m.syncChoice()
}

// templateQuestion puts the question of t in its list, a short menu on the answer at selected: empty or from
// an existing entry, or, for the kind, a credential or a payload secret.
func (m *Model) templateQuestion(t *templateChoice, selected int) error {
	noun := entryWord("connection")
	if !t.setup {
		noun = m.section.entry()
	}
	title, column := tr(tLeaveNew, noun), columnID("template")
	if t.setup {
		title = tr(tTemplateGuidedSetup)
	}
	rows := []string{tr(tFormTemplateEmpty), tr(tFormTemplateExisting, noun)}
	if t.kind {
		title, column = tr(tTemplateKindTitle), columnID("Kind")
		rows = []string{tr(tTemplateKindCredential), tr(tTemplateKindPayload)}
	}
	items := make([]kittui.ListItem, len(rows))
	for i, row := range rows {
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: []string{row}}
	}
	list, err := newChoice(kittui.ListSpec{Columns: []kittui.Column{{Title: column}}, Search: kittui.SearchNone,
		Wrap: true, Empty: tTemplateNoEntryMatchesEscGoes}, title, items, selected, "{enter} choose", "{back} cancel")
	if err == nil {
		t.list, t.picking, t.names = list, false, nil
	}
	return err
}

// templateEntries puts the entries to copy in the list of t, with the search typed at once. A name is a text
// of the configuration, so it is drawn through the log filter.
func (m *Model) templateEntries(t *templateChoice) error {
	names := m.templateNames(t.setup)
	noun, detail := m.section.entry(), columnID("details")
	if t.setup {
		noun, detail = entryWord("connection"), columnID("SERVICE")
	}
	items := make([]kittui.ListItem, len(names))
	for i, name := range names {
		var rest []string
		search := name
		if t.setup {
			rest = []string{m.cfg.Connections[name].Service}
		} else {
			rest = m.cells(name)[1:]
			if groupedSection(m.section) {
				rest = append([]string{m.providerOf(m.section)(name)}, rest...)
			}
			search = m.describe(name)
		}
		items[i] = kittui.ListItem{ID: choiceID(i), Cells: []string{m.logText(name), m.logText(strings.Join(rest, columnGap))},
			SearchText: m.logText(search)}
	}
	list, err := newChoice(kittui.ListSpec{Columns: []kittui.Column{{Title: columnID("NAME")},
		{Title: detail, Flex: true, Dimmed: true}}, Search: kittui.SearchAsTyped, Wrap: true,
		Empty: tTemplateNoEntryMatchesEscGoes}, tr(tTemplateNewFromExisting, noun), items, 0, "{enter} choose", "{back} back")
	if err == nil {
		t.list, t.picking, t.names = list, true, names
	}
	return err
}

// askSetupTemplate is g: the guided setup starts at once without a connection to copy, and otherwise asks
// whether to start empty or from an existing connection first.
func (m *Model) askSetupTemplate() {
	if len(m.cfg.Connections) == 0 {
		m.startSetup()
		return
	}
	m.clearMessages()
	if len(m.cfg.Providers()) == 0 {
		m.fail = tr(tSetupThisBuildRegistersNoProvider)
		return
	}
	m.askTemplate(true)
}

// templateFromScreen is where the question was opened from, never a stale template screen.
func (m *Model) templateFromScreen() screen {
	if m.template != nil && m.template.from != screenTemplate {
		return m.template.from
	}
	return screenList
}

// duplicateSelected is p on a list: the form of a new entry filled from the marked one.
func (m *Model) duplicateSelected() tea.Cmd {
	if !m.templateSection(m.section) {
		return nil
	}
	name, ok := m.selected()
	if !ok {
		return nil
	}
	return m.openCopy(name)
}

// openCopy opens the form of a new entry filled from source. A new agent token is a managing action from
// the start, so it asks for the admin session first, like n does.
func (m *Model) openCopy(source string) tea.Cmd {
	if m.section == sectionTokens {
		return m.requireAdmin(func() tea.Cmd { return m.openEntry("", source) })
	}
	return m.openEntry("", source)
}

// copyName is the name suggested for a copy of source: source-copy, or source-copy-N with the lowest N from
// 2 on that is still free in the active section.
func (m *Model) copyName(source string) string {
	taken := map[string]bool{}
	for _, name := range m.entryNames(m.section) {
		taken[name] = true
	}
	return freeCopyName(source, taken)
}

func freeCopyName(source string, taken map[string]bool) string {
	name := source + "-copy"
	for n := 2; taken[name]; n++ {
		name = source + "-copy-" + strconv.Itoa(n)
	}
	return name
}

// sourceEntry is the entry whose values the form being applied carries: the copied one for a new entry
// filled from another, else the entry itself.
func (m *Model) sourceEntry(name string) string {
	if m.editing == "" && m.copyFrom != "" {
		return m.copyFrom
	}
	return name
}

// copyNote is the line under a copied form.
func (m *Model) copyNote() string {
	note := tr(tTemplateEverythingFromButTheName, m.copyFrom)
	switch m.section {
	case sectionCredentials:
		note = tr(tTemplateStoragePlaceOfNeverA, m.copyFrom)
	case sectionTokens:
		note = tr(tTemplateVorbilderAndAFutureExpiry, m.copyFrom)
	}
	return note
}

// templateAnswer runs what the list of screenTemplate answers. Enter on the question opens the form, the
// entries or the template question of a credential; on an entry it fills the form of a new entry from it.
// Back goes from the entries to the question and from the question to where it was asked.
func (m *Model) templateAnswer(act action) tea.Cmd {
	t := m.template
	if t == nil {
		return nil
	}
	leave := func() {
		m.template = nil
		m.screen = t.from
	}
	n := 2
	if t.picking {
		n = len(t.names)
	}
	at, ok := chosen(t.list, n)
	var err error
	switch {
	case act == actBack && t.picking:
		err = m.templateQuestion(t, 1)
	case act == actBack:
		leave()
	case act != actEnter || !ok:
	case t.picking:
		leave()
		if t.setup {
			m.startSetupFrom(t.names[at])
			return nil
		}
		return m.openCopy(t.names[at])
	case t.kind && at == 1:
		leave()
		return m.openPayloadNew()
	case t.kind && m.templateOffered():
		t.kind = false
		err = m.templateQuestion(t, 0)
	case t.kind:
		leave()
		return m.newEmptyEntry()
	case at == 1:
		err = m.templateEntries(t)
	default:
		leave()
		if t.setup {
			m.startSetup()
			return nil
		}
		return m.newEmptyEntry()
	}
	if err != nil {
		leave()
		m.fail = err.Error()
	}
	return nil
}

// startSetupFrom starts the guided setup past its provider step, on the provider of the named connection,
// with the rows of the later steps filled from it. The setup still writes nothing before its last step, and
// the last step is still saved through requireAdmin.
func (m *Model) startSetupFrom(source string) {
	m.clearMessages()
	conn, ok := m.cfg.Connections[source]
	provider := m.cfg.Services[conn.Service].Provider
	if !ok || provider == "" {
		m.fail = tr(tTemplateConnectionNamesNoProviderTo, source)
		return
	}
	m.stopTest()
	m.testName = ""
	m.wizard = &setup{provider: provider, template: source}
	m.wizard.pages = [][]field{m.setupPage(stepProvider)}
	m.setupShow(stepService)
}

// setupPrefill fills the rows of a step the setup has just built from the connection it started from. The
// credential row only names an existing credential: the setup copies no secret.
func (m *Model) setupPrefill(step int, page []field) {
	w := m.wizard
	conn, ok := m.cfg.Connections[w.template]
	if !ok {
		return
	}
	conn = conn.WithoutOutdatedTargets()
	switch step {
	case stepService:
		if slices.Contains(page[0].choices, conn.Service) {
			page[0] = choiceField("service", page[0].choices, conn.Service).withHint(page[0].hint).withDetail(page[0].detail).inSection(page[0].section)
		}
	case stepCredential:
		if slices.Contains(page[0].choices, conn.Credential) {
			page[0] = choiceField("credential", page[0].choices, conn.Credential).withHint(page[0].hint).withDetail(page[0].detail).inSection(page[0].section)
		}
	case stepScope:
		taken := map[string]bool{}
		for name := range m.cfg.Connections {
			taken[name] = true
		}
		// The rows stand grouped by section, so they are found by their label.
		for i := range page {
			switch page[i].label {
			case nameLabel:
				page[i].input.SetValue(freeCopyName(w.template, taken))
			case targetsLabel:
				// A copy is never broader than its template: the exclusions come along.
				page[i] = targetsFieldOf(conn).withHint(page[i].hint).withDetail(page[i].detail).inSection(page[i].section)
				page[i].labels = nil
			case descriptionLabel:
				page[i].input.SetValue(conn.Description)
			}
		}
	case stepEffects:
		for _, row := range m.accessFields(w.provider, conn.Effects, slices.Clone(conn.Tools)) {
			for i := range page {
				if page[i].label == row.label {
					page[i] = row.inSection(page[i].section)
				}
			}
		}
	}
}

// setupTemplateExtras are what the setup has no row for and copies from its template unchanged: the paths
// and local file lists of the connection, and its target labels.
func (m *Model) setupTemplateExtras() ([]string, config.Files, map[string]string) {
	source, ok := m.cfg.Connections[m.wizard.template]
	if !ok {
		return nil, config.Files{}, nil
	}
	return slices.Clone(source.Paths), config.Files{Read: slices.Clone(source.Files.Read), Write: slices.Clone(source.Files.Write)},
		maps.Clone(source.TargetLabels)
}
