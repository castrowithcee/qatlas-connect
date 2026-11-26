package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// action names a key-bound action of the editor. Dispatch and footers refer to the action, never to the
// key, so the binding lives in exactly one place.
type action = kit.ActionID

const (
	actInfo      action = kit.ActionInfo
	actClone     action = kit.ActionClone
	actReload    action = kit.ActionReload
	actMenu      action = kit.ActionMenu
	actSave      action = "save"
	actHelp      action = "help"
	actHelpF9    action = kit.ActionHelpF9
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
	actDecline    action = "decline"
	actBlock      action = "block"
	actTickAll    action = "tickall"
	actEdit       action = "edit"
	actRename     action = "rename"
	actReveal     action = "reveal"
	actSummary    action = "summary"
	actFilter     action = "filter"
	actRevert     action = "revert"
	actSessions   action = "sessions"
	actLogs       action = "logs"
	actTest       action = "test"
	actUpdate     action = "update"
	actWrite      action = "write"

	actRemoveField action = "removefield"
	actSuggestPrev action = "suggestprev"
	actSuggestNext action = "suggestnext"
	actNextKind    action = "nextkind"
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
var keyTable = slices.Concat(kit.StandardActions(), []kit.Action{

	// The letter actions of Connect: each letter means one thing on every screen where nothing is typed.
	row(actSetup, kit.LevelLetter, tKeyHelpSetup, "g"),
	row(actApproveAll, kit.LevelLetter, tKeyHelpApproveAll, "A"),
	row(actDecline, kit.LevelLetter, tKeyHelpDecline, "x"),
	row(actBlock, kit.LevelLetter, tKeyHelpBlock, "b"),
	row(actTickAll, kit.LevelLetter, tKeyHelpTickAll, "a"),
	row(actReveal, kit.LevelLetter, tKeyHelpReveal, "V"),
	row(actSummary, kit.LevelLetter, tKeyHelpSummary, "s"),
	row(actRevert, kit.LevelLetter, tKeyHelpRevert, "B"),
	row(actSessions, kit.LevelLetter, tKeyHelpSessions, "M"),
	row(actLogs, kit.LevelLetter, tKeyHelpLogs, "o"),
	row(actTest, kit.LevelLetter, tKeyHelpTest, "t"),
	row(actWrite, kit.LevelLetter, tKeyHelpWrite, "W"),

	// The control keys of text lines and F8 of the payload form. None can be rebound to a global action.
	row(actRemoveField, kit.LevelCtrl, tKeyHelpRemoveField, "f8"),
	secondWayOf(row(actSuggestPrev, kit.LevelCtrl, tKeyHelpSuggestPrev, "ctrl+p"), actUp),
	secondWayOf(row(actSuggestNext, kit.LevelCtrl, tKeyHelpSuggestNext, "ctrl+n"), actDown),
	row(actNextKind, kit.LevelCtrl, tKeyHelpNextKind, "ctrl+y"),

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
})

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

// applyKeys makes the keys of the preferences the keymap. Dispatch and footers read the keymap alone, so
// both follow at once. The preferences are checked before they get here; a set the table still refuses
// leaves the default keys.
func applyKeys(keys map[action]string) {
	if err := keymap.Bind(keys); err != nil {
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

// namedCodes are the keys a key table names by a word.
var namedCodes = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace, "backspace": tea.KeyBackspace,
	"delete": tea.KeyDelete, "insert": tea.KeyInsert, "up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft,
	"right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
}

// namedKey is the key press a key name of the table stands for, as a terminal reports it; false for a name it
// cannot build back.
func namedKey(name string) (tea.KeyPressMsg, bool) {
	var key tea.KeyPressMsg
	base := name
	for _, mod := range []struct {
		prefix string
		mod    tea.KeyMod
	}{{"ctrl+", tea.ModCtrl}, {"alt+", tea.ModAlt}, {"shift+", tea.ModShift}} {
		if rest, ok := strings.CutPrefix(base, mod.prefix); ok && rest != "" {
			key.Mod, base = key.Mod|mod.mod, rest
		}
	}
	if code, ok := namedCodes[base]; ok {
		key.Code = code
		if base == "space" && key.Mod == 0 {
			key.Text = " "
		}
	} else if n, err := strconv.Atoi(strings.TrimPrefix(base, "f")); err == nil && strings.HasPrefix(base, "f") && n >= 1 && n <= 20 {
		key.Code = tea.KeyF1 + rune(n-1)
	} else if runes := []rune(base); len(runes) == 1 {
		key.Code = runes[0]
		if key.Mod == 0 {
			key.Text = base
		}
	}
	return key, key.Code != 0 && kit.KeyName(key) == name
}

// footerPlaceholder is the name a footer text uses for the keys of an action, e.g. "{save} save": the action
// in braces, so every action of the table can be named and none by hand.
func footerPlaceholder(a action) string { return "{" + string(a) + "}" }
