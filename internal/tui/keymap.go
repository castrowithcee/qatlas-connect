package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
)

// action names a key-bound action of the editor. Dispatch and footers refer to the action, never to the
// key, so the binding lives in exactly one place.
type action = kit.ActionID

const (
	actInfo      action = "info"
	actSave      action = "save"
	actHelp      action = "help"
	actCommand   action = "command"
	actLock      action = "lock"
	actBack      action = "back"
	actDelete    action = "delete"
	actForceQuit action = "forcequit"

	actUp       action = "up"
	actDown     action = "down"
	actLeft     action = "left"
	actRight    action = "right"
	actHome     action = "home"
	actEnd      action = "end"
	actPageUp   action = "pageup"
	actPageDown action = "pagedown"
	actNext     action = "next"
	actPrev     action = "prev"
	actEnter    action = "enter"
	actSpace    action = "space"
	actSearch   action = "search"
	actQuit     action = "quit"
	actArea     action = "area"

	actVimUp    action = "vimup"
	actVimDown  action = "vimdown"
	actVimLeft  action = "vimleft"
	actVimRight action = "vimright"

	actTopicNext action = "topicnext"
	actTopicPrev action = "topicprev"

	actYes     action = "yes"
	actNo      action = "no"
	actDiscard action = "discard"

	actSetup      action = "setup"
	actCopy       action = "copy"
	actNew        action = "new"
	actApproveAll action = "approveall"
	actEdit       action = "edit"
	actRename     action = "rename"
	actReveal     action = "reveal"
	actSummary    action = "summary"
	actFilter     action = "filter"
	actRevert     action = "revert"
	actSessions   action = "sessions"
	actLogs       action = "logs"
	actDuplicate  action = "duplicate"
	actTest       action = "test"
	actUpdate     action = "update"
	actWrite      action = "write"

	actRemoveField action = "removefield"
	actSuggestPrev action = "suggestprev"
	actSuggestNext action = "suggestnext"
	actNextKind    action = "nextkind"
	actReload      action = "reload"
)

// row is one entry of the key table with fixed keys.
func row(id action, level kit.KeyLevel, help textID, keys ...string) kit.Action {
	return kit.Action{ID: id, Level: level, DefaultKeys: keys, Help: kit.TextID(help)}
}

// rebindable marks an entry as one the preferences may change.
func rebindable(a kit.Action, how kit.Rebinding) kit.Action { a.Rebind = how; return a }

// secondWayOf marks an entry as the second way of another action: the control keys of text lines beside the
// arrow keys, which terminals and multiplexers would otherwise take.
func secondWayOf(a kit.Action, first action) kit.Action { a.SecondWayOf = first; return a }

// keyTable is the key table of the editor: every action with its level, default keys, and the catalog text
// of its help, as the kit checks and binds it. The vim letters are entries of their own that only moveKey
// reads: on a screen that reads text they are text, so the movement bindings hold arrows only. The help level
// holds the keys only the help overlay knows; tab there steps through topics, not fields. Within the main
// levels no two actions share a key; across them one key may mean different things only where the levels
// never meet: the answers of a question (y, n, d) and the keys only the help overlay knows.
var keyTable = []kit.Action{
	rebindable(row(actSave, kit.LevelGlobal, tKeyHelpSave, "f2"), kit.RebindReplace),
	rebindable(row(actHelp, kit.LevelGlobal, tKeyHelpHelp, "?"), kit.RebindReplace),
	rebindable(row(actCommand, kit.LevelGlobal, tKeyHelpCommand, ":"), kit.RebindReplace),
	rebindable(row(actLock, kit.LevelGlobal, tKeyHelpLock, "ctrl+l"), kit.RebindReplace),
	row(actBack, kit.LevelGlobal, tKeyHelpBack, "esc"),
	row(actForceQuit, kit.LevelGlobal, tKeyHelpForceQuit, "ctrl+c"),

	// The letter actions: each letter means one thing on every screen where nothing is typed. The info letter
	// takes an optional second key that, unlike the letter, also works on text lines.
	rebindable(row(actInfo, kit.LevelLetter, tKeyHelpInfo, "i"), kit.RebindExtra),
	row(actDelete, kit.LevelLetter, tKeyHelpDelete, "d"),
	row(actQuit, kit.LevelLetter, tKeyHelpQuit, "q"),
	row(actSetup, kit.LevelLetter, tKeyHelpSetup, "g"),
	row(actCopy, kit.LevelLetter, tKeyHelpCopy, "c"),
	row(actNew, kit.LevelLetter, tKeyHelpNew, "n"),
	row(actApproveAll, kit.LevelLetter, tKeyHelpApproveAll, "A"),
	row(actEdit, kit.LevelLetter, tKeyHelpEdit, "e"),
	row(actRename, kit.LevelLetter, tKeyHelpRename, "r"),
	row(actReveal, kit.LevelLetter, tKeyHelpReveal, "V"),
	row(actSummary, kit.LevelLetter, tKeyHelpSummary, "s"),
	row(actFilter, kit.LevelLetter, tKeyHelpFilter, "f"),
	row(actRevert, kit.LevelLetter, tKeyHelpRevert, "B"),
	row(actSessions, kit.LevelLetter, tKeyHelpSessions, "M"),
	row(actLogs, kit.LevelLetter, tKeyHelpLogs, "o"),
	row(actDuplicate, kit.LevelLetter, tKeyHelpDuplicate, "p"),
	row(actTest, kit.LevelLetter, tKeyHelpTest, "t"),
	row(actUpdate, kit.LevelLetter, tKeyHelpUpdate, "U"),
	row(actWrite, kit.LevelLetter, tKeyHelpWrite, "W"),

	// The control keys of text lines and of the target picker, and F8 of the payload form. None can be rebound to a global action.
	row(actRemoveField, kit.LevelCtrl, tKeyHelpRemoveField, "f8"),
	secondWayOf(row(actSuggestPrev, kit.LevelCtrl, tKeyHelpSuggestPrev, "ctrl+p"), actUp),
	secondWayOf(row(actSuggestNext, kit.LevelCtrl, tKeyHelpSuggestNext, "ctrl+n"), actDown),
	row(actNextKind, kit.LevelCtrl, tKeyHelpNextKind, "ctrl+y"),
	row(actReload, kit.LevelCtrl, tKeyHelpReload, "ctrl+r"),

	row(actUp, kit.LevelMove, tKeyHelpUp, "up"),
	row(actDown, kit.LevelMove, tKeyHelpDown, "down"),
	row(actLeft, kit.LevelMove, tKeyHelpLeft, "left"),
	row(actRight, kit.LevelMove, tKeyHelpRight, "right"),
	row(actHome, kit.LevelMove, tKeyHelpHome, "home"),
	row(actEnd, kit.LevelMove, tKeyHelpEnd, "end"),
	row(actPageUp, kit.LevelMove, tKeyHelpPageUp, "pgup"),
	row(actPageDown, kit.LevelMove, tKeyHelpPageDown, "pgdown"),
	row(actNext, kit.LevelMove, tKeyHelpNext, "tab"),
	row(actPrev, kit.LevelMove, tKeyHelpPrev, "shift+tab"),
	row(actEnter, kit.LevelMove, tKeyHelpEnter, "enter"),
	row(actSpace, kit.LevelMove, tKeyHelpSpace, "space"),
	row(actSearch, kit.LevelMove, tKeyHelpSearch, "/"),
	row(actArea, kit.LevelMove, tKeyHelpArea, areaKeys()...),

	row(actVimUp, kit.LevelVim, tKeyHelpUp, "k"),
	row(actVimDown, kit.LevelVim, tKeyHelpDown, "j"),
	row(actVimLeft, kit.LevelVim, tKeyHelpLeft, "h"),
	row(actVimRight, kit.LevelVim, tKeyHelpRight, "l"),

	row(actTopicNext, kit.LevelHelp, tKeyHelpTopicNext, "tab"),
	row(actTopicPrev, kit.LevelHelp, tKeyHelpTopicPrev, "shift+tab"),

	row(actYes, kit.LevelConfirm, tKeyHelpYes, "y"),
	row(actNo, kit.LevelConfirm, tKeyHelpNo, "n"),
	row(actDiscard, kit.LevelConfirm, tKeyHelpDiscard, "d"),
}

// areaKeys are the digits that open the areas, one per area in the order of the tab line.
func areaKeys() []string {
	var keys []string
	for a := area(0); a < areaCount; a++ {
		keys = append(keys, strconv.Itoa(int(a)+1))
	}
	return keys
}

// newKeymap checks table and binds its default keys. The arrows are drawn as glyphs; a terminal without
// them gets the footer's own ASCII conversion.
func newKeymap(table []kit.Action) (*kit.Keymap, error) {
	return kit.NewKeymap(table, kit.CharsetUnicode)
}

// keymap is the key table in force. Dispatch and footers read it alone; applyKeys is the only writer.
var keymap = func() *kit.Keymap {
	km, err := newKeymap(keyTable)
	if err != nil {
		panic("the key table is invalid: " + err.Error())
	}
	return km
}()

// userActions maps the rebindable actions of the preferences to the actions of the keymap.
var userActions = map[string]action{
	uiprefs.ActionSave: actSave, uiprefs.ActionHelp: actHelp, uiprefs.ActionCommand: actCommand, uiprefs.ActionLock: actLock,
}

func init() { uiprefs.Reserved = reservedOwner }

// reservedOwner names the action of the table that owns k and that the preferences cannot replace, so no
// binding may take it; "" means the key is free. The info letter is such an owner: its second key is a
// preference, the letter is not.
func reservedOwner(k string) string {
	for _, a := range keymap.Actions() {
		if a.Rebind != kit.RebindReplace && slices.Contains(a.DefaultKeys, k) {
			return string(a.ID)
		}
	}
	return ""
}

// applyKeys makes the keys of the preferences the keymap. Dispatch and footers read the keymap alone, so
// both follow at once. The preferences are checked before they get here; a set the table still refuses
// leaves the default keys.
func applyKeys(k uiprefs.Keys) {
	k = k.WithDefaults()
	overrides := map[action]string{actInfo: k.Info}
	for name, a := range userActions {
		overrides[a] = k.Get(name)
	}
	if err := keymap.Bind(overrides); err != nil {
		_ = keymap.Bind(nil)
	}
}

// keyIs reports whether the key press triggers the action, where every key of the table acts: the screens
// decide themselves where text is typed.
func keyIs(msg tea.KeyPressMsg, a action) bool { return keymap.Is(msg, a, kit.InputNavigation) }

// keyLabel is how a footer names the keys of an action, joined by "/": function keys capitalized,
// everything else as bound, the second info key last. A run of three or more digits is written as a range.
func keyLabel(a action) string { return keymap.Label(a) }

// infoExtraLabel is the second info key, which also acts on text lines, as a footer names it; "" means none.
func infoExtraLabel() string {
	label, _ := keymap.Labels(kit.InputText).KeyLabel(actInfo)
	return label
}

// tableKey is the first default key of an action of the table.
func tableKey(a action) string {
	for _, e := range keyTable {
		if e.ID == a {
			return e.DefaultKeys[0]
		}
	}
	return ""
}

// keyPress is the key press a key name of the table stands for, for the keys a footer button may send.
func keyPress(name string) (tea.KeyPressMsg, bool) {
	switch name {
	case keyLabel(actEnter):
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case keyLabel(actBack):
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	case keyLabel(actNext):
		return tea.KeyPressMsg{Code: tea.KeyTab}, true
	}
	if len(name) >= 2 && (name[0] == 'F' || name[0] == 'f') {
		if n, err := strconv.Atoi(name[1:]); err == nil && n >= 1 && n <= 12 {
			return tea.KeyPressMsg{Code: tea.KeyF1 + rune(n-1)}, true
		}
	}
	if runes := []rune(name); len(runes) == 1 && runes[0] > ' ' && runes[0] < 0x7f && !(runes[0] >= '0' && runes[0] <= '9') {
		return tea.KeyPressMsg{Code: runes[0], Text: name}, true
	}
	return tea.KeyPressMsg{}, false
}

// footerPlaceholder is the name a footer text uses for the keys of an action, e.g. "{save} save": the action
// in braces, so every action of the table can be named and none by hand.
func footerPlaceholder(a action) string { return "{" + string(a) + "}" }

// footerKeys replaces the key placeholders of a footer text with the current key names, so a footer
// follows the keymap while a screen-local key stays a plain literal in its text.
func footerKeys(text string) string { return keyPlaceholders(footerText(text)) }

// keyPlaceholders replaces the key placeholders of an already translated footer text. Translating comes
// first because the catalog is keyed by the group with its placeholders.
func keyPlaceholders(text string) string {
	if !strings.Contains(text, "{") {
		return text
	}
	for _, e := range keyTable {
		if p := footerPlaceholder(e.ID); strings.Contains(text, p) {
			text = strings.ReplaceAll(text, p, keyLabel(e.ID))
		}
	}
	return text
}
