package tui

import (
	"regexp"
	"slices"
	"strings"

	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// The key line of a screen is data before it is drawn: screenKeyText is the text the screen hands to the
// footer, screenHints the same line as the kit's hints. The views draw from the same functions, so the two
// cannot drift apart.

// Key texts that more than one view draws.
const (
	keysBackOnly      = "{back} back · {command} cmds · {help} help"
	keysScroll        = "{up}/{down} scroll · "
	keysTokenDetail   = "{reveal} reveal/hide · {delete} revoke · {back} back · {command} cmds · {help} help"
	keysRequestBody   = "{up}/{down} move · {space} tick · {tickall} all or none · {yes} accept · {decline} decline · {block} block · {back} back · {command} cmds · {help} help"
	keysRequestChoice = "{up}/{down} choose · {enter} accept · {back} back · {command} cmds · {help} help"
	keysCommandLine   = "type to search · {up}/{down} or {next} choose · {enter} run · {back} close"
	keysLogDetail     = "{back} back · {area} section · {command} cmds · {help} help"
	keysTemplateKind  = "{up}/{down} move · {enter} choose · {back} cancel · {command} cmds · {help} help"

	keysExportSelect = "{up}/{down} move · {space} tick · {save} or {enter} next · {back} cancel · {command} cmds · {help} help"
	keysExportMode   = "{up}/{down} move · {save} or {enter} choose · {back} back · {command} cmds · {help} help"
	keysExportPlain  = "{yes} write unencrypted · {no}/{back} back, nothing written · {command} cmds · {help} help"

	keysTransferPreview = "{save} write · {back} back · {up}/{down} scroll · {command} cmds · {help} help"
	keysTransferDone    = "{enter} or {back} done · {up}/{down} scroll · {command} cmds · {help} help"

	keysImportMode    = "{up}/{down} move · {save} or {enter} preview · {back} back · {command} cmds · {help} help"
	keysImportNothing = "{back} back · {up}/{down} scroll · {command} cmds · {help} help"
	keysImportNext    = "{enter}, {save} or {back} next question · {up}/{down} scroll · {command} cmds · {help} help"
	keysImportApprove = "{yes} approve · {no}/{back} not now, nothing is approved · {up}/{down} scroll · {command} cmds · {help} help"
	keysImportDelete  = "{yes} delete the file · {no}/{back} keep it · {up}/{down} scroll · {command} cmds · {help} help"
)

// screenKeyText is the key text the current screen puts in the footer, known before anything is drawn. A
// screen that a modal dialog covers draws its keys in the dialog, so this is the line of the workspace form.
func (m *Model) screenKeyText() string {
	var text string
	m.asDrawn(func() { text = m.workspaceKeys() })
	if text == "" {
		return ""
	}
	return m.withCopyKeyShown(m.withInfoKey(text))
}

// screenPosition is the position in the list of the current screen that the footer shows at its right end,
// known before anything is drawn.
func (m *Model) screenPosition() string {
	var pos string
	m.asDrawn(func() { pos = m.logPosition() })
	return pos
}

// asDrawn runs build in the room the current screen is drawn in; a terminal too small for the frame draws no
// screen.
func (m *Model) asDrawn(build func()) {
	if m.quitting || m.terminalTooSmall() {
		return
	}
	if m.paneShown() {
		// The screen draws beside its pane, in less width, and the pane changes what its keys cost.
		full, in := m.width, m.inPane
		m.width, m.inPane = full-helpPaneWidth, true
		defer func() { m.width, m.inPane = full, in }()
	}
	build()
}

// workspaceKeys is the key text of the current screen before the keys every screen adds.
func (m *Model) workspaceKeys() string {
	screen := m.screen
	if m.pagePreview() {
		screen = screenForm
	}
	switch screen {
	case screenNav, screenList:
		switch m.section {
		case sectionLogs:
			return m.logsKeys()
		case sectionOverview:
			return m.overviewKeys()
		}
		return m.listKeys()
	case screenLogs:
		return m.logsKeys()
	case screenLogDetail:
		return m.logDetailKeys()
	case screenPicker:
		return m.pickerKeys()
	case screenProviders:
		return m.providerKeys()
	case screenCommand:
		return keysCommandLine
	case screenSelftest:
		if m.st == nil {
			return ""
		}
		return m.selftestKeys()
	case screenUpdate:
		return m.questionKeys(m.updateView)
	case screenRestart:
		return m.questionKeys(m.restartView)
	case screenLeave:
		return m.questionKeys(m.leaveView)
	case screenVersions:
		if m.versions == nil {
			return ""
		}
		return m.versionKeys()
	case screenTargets:
		switch {
		case m.targetPick != nil:
			return m.pickKeys()
		case m.targetAdd != nil:
			return m.addKeys()
		}
		return m.targetKeys()
	case screenPaths:
		return m.pathKeys()
	case screenTemplate:
		if m.template == nil {
			return ""
		}
		return m.templateKeys()
	case screenRename:
		if m.rename == nil || m.rename.plan == nil {
			return ""
		}
		return m.renameQuestion().keys
	case screenFindings:
		return m.findingsKeys()
	case screenTransfer:
		if m.imp != nil {
			return m.importKeyText()
		}
		return m.exportKeys()
	case screenForm:
		if m.fields == nil {
			return ""
		}
		return m.formScreenKeys()
	case screenSecret, screenVaultOffer, screenAdminAuth, screenVaultUnlock:
		d, _ := m.prompt()
		return d.keys
	case screenConfirm:
		return m.confirmKeys()
	case screenSummary:
		if m.wizard == nil {
			return ""
		}
		return m.summaryKeys()
	}
	return ""
}

// logsKeys is the key text of the Logs screen: its date and filter dialogs, or the table.
func (m *Model) logsKeys() string {
	lv := m.logs
	switch {
	case m.screen == screenLogs && lv.dialog != nil:
		return logDateKeys(lv.dialog)
	case m.screen == screenLogs && lv.pick != nil:
		return m.logPickKeys()
	}
	return m.logKeys()
}

// confirmKeys is the key text of screenConfirm, which holds the questions and three detail screens.
func (m *Model) confirmKeys() string {
	switch {
	case m.settingsTakeover != nil, m.vaultLockConfirm, m.decryptConfirm, m.migratePlanConfirm, m.migrateDeleteConfirm:
	case m.requestDetail != "":
		if _, ok := m.requestByID(m.requestDetail); !ok {
			return keysBackOnly
		}
		if m.requestChoice != nil {
			return keysRequestChoice
		}
		return keysRequestBody
	case m.approvalDetail != "":
		return m.approvalDetailKeys()
	case m.approveAllConfirm, m.tokenRevoke != "":
	case m.tokenDetail != "":
		if _, ok, unavailable := m.tokenByName(m.tokenDetail); !ok || unavailable != "" {
			return keysBackOnly
		}
		return keysTokenDetail
	}
	return m.questionKeys(m.confirmView)
}

// approvalDetailKeys is the key text of the detail of an approval: a body taller than the room gains the
// scroll keys.
func (m *Model) approvalDetailKeys() string {
	change, ok := m.approvalChange(m.approvalDetail)
	if !ok {
		return keysBackOnly
	}
	lines := len(m.approvalBody(change))
	keys := m.approvalKeys(change, lines)
	room := m.height - m.detailChrome() - 1
	if lines > room {
		room = m.height - m.detailChrome() - 2
	}
	if lines > max(room, 1) {
		return keysScroll + keys
	}
	return keys
}

// questionKeys is the key text of the question a view builds, without drawing it: questionText hands over
// the keys of its question and stops there.
func (m *Model) questionKeys(build func() string) string {
	var keys string
	m.probeKeys = &keys
	defer func() { m.probeKeys = nil }()
	build()
	return keys
}

var (
	hintLead        = regexp.MustCompile(`^\{\w+\}((?:/|, | or | oder )\{\w+\})*`)
	hintPlaceholder = regexp.MustCompile(`\{(\w+)\}`)
)

// hintParts splits the raw text of a key group into the actions its leading key placeholders name and the
// label after them: "{enter}/{edit} edit" is enter and edit with the label "edit", "{enter} or {back} done"
// is enter and back with "done". A group that starts with no placeholder is a label alone.
func hintParts(raw string) (actions []action, label string) {
	lead := hintLead.FindString(raw)
	for _, name := range hintPlaceholder.FindAllStringSubmatch(lead, -1) {
		actions = append(actions, action(name[1]))
	}
	return actions, strings.TrimPrefix(raw[len(lead):], " ")
}

// hintLabelID is the ID of the label of a footer group of the catalog: the text of the group without its keys.
func hintLabelID(group string) textID { return textID("hint." + group) }

// keyHints is a key text as the kit's hints. The groups keep their order: a group of several keys becomes
// one hint per key, which Keymap.Hints folds into one group again by their shared label. The help group
// stays at every width, a group at the right end is navigation, and the answers of a question stay. A screen
// that reads text names only keys it acts on before the text, so its hints name them while it types; the
// help, command and info keys follow their own reach.
func (m *Model) keyHints(text string) []kit.Hint {
	t := translator(infoExtraLabel() != "" && !m.infoReachable())
	answering := m.questionScreen() || m.promptScreen()
	typed := func(a action) bool {
		switch a {
		case actHelp, actCommand:
			return m.helpReachable()
		case actInfo:
			return m.infoReachable()
		}
		return true
	}
	resolved := strings.Split(footerResolve(t, text), " · ")
	var hints []kit.Hint
	seen := map[string]bool{}
	for i, group := range strings.Split(text, " · ") {
		if group == "" || seen[resolved[i]] {
			continue
		}
		seen[resolved[i]] = true
		raw, inCatalog := t.Find(footerID(group))
		if !inCatalog {
			// A group built from a name has no catalog text: it stands as written, like the footer draws it.
			raw = group
		}
		actions, label := hintParts(raw)
		labelID := textID(label)
		if inCatalog && label != "" {
			labelID = hintLabelID(group)
		}
		rank, right := footerGroupRank(resolved[i], i)
		if len(actions) == 1 && listMove(actions[0], label) {
			// The up and down keys of a list stand apart, each with the kit's label, and move.
			rank, right = footerNavigate+i, true
		}
		kind := kit.HintNormal
		switch {
		case rank == footerHelp:
			kind = kit.HintAlways
		case right:
			kind = kit.HintNavigation
		case answering && rank != footerCommand && rank != footerQuit && rank != footerInfo:
			kind = kit.HintAnswer
		}
		if len(actions) == 0 {
			hints = append(hints, kit.Hint{Label: labelID, Priority: rank, Kind: kind})
		}
		for _, a := range actions {
			hints = append(hints, kit.Hint{Action: a, Label: labelID, Priority: rank, Kind: kind, WhileTyping: typed(a)})
		}
	}
	// A screen that names no keys at all gets the help key.
	if len(hints) == 0 && keyLabel(actHelp) != "" {
		hints = append(hints, kit.Hint{Action: actHelp, Label: tHeaderHelp, Priority: footerHelp, Kind: kit.HintAlways, WhileTyping: typed(actHelp)})
	}
	return hints
}

// listMove reports whether a group of action a and label is the up or down hint of a list (see listHintText).
func listMove(a action, label string) bool {
	return (a == actUp && label == tr(kit.TextKeyUp)) || (a == actDown && label == tr(kit.TextKeyDown))
}

// screenHints is the key line of the current screen as the kit's hints, before anything is drawn.
func (m *Model) screenHints() []kit.Hint { return m.keyHints(m.screenKeyText()) }

// Ranks of the groups of a key line, lowest stays longest. Groups that match no rule rank by position in the
// screen's own text, earlier first; every rank below footerOthers is kept ahead of them, every rank above
// is dropped before them.
const (
	footerHelp     = 0 // never dropped
	footerBack     = 1
	footerLock     = 2 // the only way to learn the lock key
	footerPrimary  = 3 // enter, y, n, the delete key, the digits of the areas and the filter key
	footerSave     = 4
	footerCommand  = 5
	footerQuit     = 6
	footerOthers   = 10
	footerInfo     = 700
	footerCopy     = 710  // goes before the info key does
	footerNavigate = 1000 // navigation stands right and goes first
)

// footerGroupRank is the rank of a group. Order of the rules is the order of importance.
func footerGroupRank(group string, index int) (rank int, right bool) {
	key, _, _ := strings.Cut(group, " ")
	switch {
	case key == keyLabel(actHelp) && key != "":
		return footerHelp, false
	case isNavigation(key):
		return footerNavigate + index, true
	case slices.Contains(strings.Split(key, "/"), keyLabel(actBack)):
		return footerBack, false
	case key == keyLabel(actSave):
		return footerSave, false
	case key == keyLabel(actQuit):
		return footerQuit, false
	case slices.Contains(strings.Split(key, "/"), keyLabel(actEnter)) || key == keyLabel(actYes) || key == keyLabel(actNo) || key == keyLabel(actFilter) || key == keyLabel(actDelete) || isSectionRange(key):
		return footerPrimary, false
	case key == keyLabel(actLock):
		return footerLock, false
	case key == keyLabel(actCommand):
		return footerCommand, false
	case key == keyLabel(actInfo) || key == tableKey(actInfo):
		return footerInfo, false
	case key == keyLabel(actCopy):
		return footerCopy, false
	}
	return footerOthers + index, false
}

// isSectionRange reports whether a key is a digit range ("1-6"), which opens an area.
func isSectionRange(key string) bool {
	return len(key) >= 3 && key[0] >= '0' && key[0] <= '9' && key[1] == '-'
}

// isNavigation reports whether the first word of a group names moving through the screen rather than
// acting on it.
func isNavigation(key string) bool {
	for _, pair := range [][2]action{{actUp, actDown}, {actLeft, actRight}, {actPageUp, actPageDown}, {actHome, actEnd}} {
		if key == keyLabel(pair[0])+"/"+keyLabel(pair[1]) {
			return true
		}
	}
	return key == keyLabel(actNext)
}
