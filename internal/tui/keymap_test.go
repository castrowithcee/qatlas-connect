package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
)

// rebind gives an action another default key for one test: the table is checked again by the kit, so the key
// must be one a table may hold.
func rebind(t *testing.T, a action, key string) {
	t.Helper()
	keepKeys(t)
	table := make([]kit.Action, len(keyTable))
	for i, e := range keyTable {
		if e.ID == a {
			e.DefaultKeys = []string{key}
		}
		table[i] = e
	}
	km, err := newKeymap(table)
	if err != nil {
		t.Fatalf("rebind %s to %q: %v", a, key, err)
	}
	keyTable, keymap = table, km
}

func TestDefaultKeymapKeepsTheKnownKeys(t *testing.T) {
	want := map[action]string{
		actInfo: "i", actSave: "f2", actHelp: "?", actCommand: ":", actLock: "ctrl+l", actBack: "esc", actDelete: "d",
		actForceQuit: "ctrl+c", actUp: "up", actDown: "down", actLeft: "left", actRight: "right", actHome: "home",
		actEnd: "end", actPageUp: "pgup", actPageDown: "pgdown", actNext: "tab", actPrev: "shift+tab",
		actVimUp: "k", actVimDown: "j", actVimLeft: "h", actVimRight: "l",
		actTopicNext: "tab", actTopicPrev: "shift+tab",
		actEnter: "enter", actSpace: "space", actSearch: "/", actQuit: "q", actYes: "y", actNo: "n", actDiscard: "d",
		actSetup: "g", actCopy: "c", actNew: "n", actApproveAll: "A", actEdit: "e", actRename: "r", actReveal: "V",
		actSummary: "s", actFilter: "f", actRevert: "B", actSessions: "M", actLogs: "o", actDuplicate: "p",
		actTest: "t", actUpdate: "U", actWrite: "W",
		actRemoveField: "f8", actSuggestPrev: "ctrl+p", actSuggestNext: "ctrl+n", actNextKind: "ctrl+y",
		actReload: "ctrl+r", actArea: "1",
	}
	if len(keyTable) != len(want) {
		t.Fatalf("keymap has %d actions, want %d", len(keyTable), len(want))
	}
	for a, key := range want {
		if tableKey(a) != key {
			t.Errorf("action %q is bound to %q, want %q", a, tableKey(a), key)
		}
	}
	if !keyIs(tea.KeyPressMsg{Code: 'i', Text: "i"}, actInfo) || keyIs(tea.KeyPressMsg{}, actInfo) {
		t.Error("the info action does not match exactly its key")
	}
	if keyLabel(actSave) != "F2" || keyLabel(actHelp) != "?" {
		t.Errorf("labels = %q, %q", keyLabel(actSave), keyLabel(actHelp))
	}
}

func TestRebindingChangesFooterAndDispatchTogether(t *testing.T) {
	rebind(t, actHelp, "ctrl+x")
	rebind(t, actCommand, "f3")
	rebind(t, actSave, "f4")
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})

	view := m.render()
	if !strings.Contains(flatText(view), "F3 cmds ctrl+x help") || strings.Contains(view, "? help") || strings.Contains(view, ": cmds") {
		t.Fatalf("the footer does not follow the keymap:\n%s", view)
	}
	press(t, m, "?")
	if m.screen == screenHelp {
		t.Fatal("the old help key still opens the help")
	}
	press(t, m, ":")
	if m.screen == screenCommand {
		t.Fatal("the old command key still opens the command line")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF3})
	if m.screen != screenCommand {
		t.Fatalf("the new command key left screen %v", m.screen)
	}
	press(t, m, "esc")
	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if m.screen != screenHelp {
		t.Fatalf("the new help key left screen %v", m.screen)
	}
	if !strings.Contains(m.render(), "esc/ctrl+x close") {
		t.Errorf("the help footer does not follow the keymap:\n%s", m.render())
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if m.screen == screenHelp {
		t.Fatal("the new help key does not close the help")
	}

	press(t, m, "2", "n")
	if view := m.render(); !strings.Contains(view, "F4 save") || strings.Contains(view, "F2 save") {
		t.Fatalf("the form footer does not follow the save key:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.fail != "" || m.status != "" {
		t.Fatalf("the old save key still saves: fail %q status %q", m.fail, m.status)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF4})
	if m.fail == "" && m.status == "" {
		t.Fatal("the new save key does nothing")
	}
}

// Every key a footer names for a global action comes from the keymap: no screen file spells one as a
// literal in a footer text.
func TestFooterTextsNameGlobalKeysOnlyByPlaceholder(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{"? help", ": cmds", "F2 save", "F2 next", "F2 write", "F2 or enter", "esc back", "esc cancel", "esc close", "esc leave", "n/esc"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "keymap.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !strings.Contains(line, `"`) {
				continue
			}
			for _, b := range banned {
				if strings.Contains(line, b) && strings.Contains(line, " · ") {
					t.Errorf("%s:%d spells %q in a footer text; use the key placeholder", f, i+1, b)
				}
			}
		}
	}
}

// The key table is one the kit accepts: no action without a help text in both languages, no key two actions of
// the main levels share, no key a terminal or multiplexer takes, and a binding of the preferences keeps it so.
func TestKeyTableIsValidAndRebindingKeepsItValid(t *testing.T) {
	keepKeys(t)
	if _, err := newKeymap(keyTable); err != nil {
		t.Fatal(err)
	}
	// d is both delete and, in a question, discard; n is both new and, in a question, no: different levels.
	if tableKey(actDelete) != tableKey(actDiscard) || actDelete == actDiscard || tableKey(actNew) != tableKey(actNo) || actNew == actNo {
		t.Error("delete and discard, new and no should share a key on different levels")
	}
	// A swap of two keys is valid as a whole, a collision is refused as a whole.
	applyKeys(uiprefs.Keys{Info: "f3", Save: "f4", Help: "ctrl+x", Command: "ctrl+l", Lock: "f6"})
	if keyLabel(actCommand) != "ctrl+l" || keyLabel(actHelp) != "ctrl+x" || keyLabel(actLock) != "F6" {
		t.Errorf("the swapped keys are not in force: %s %s %s", keyLabel(actCommand), keyLabel(actHelp), keyLabel(actLock))
	}
	applyKeys(uiprefs.Keys{Save: "f4", Lock: "f4"})
	if keyLabel(actSave) != "F2" || keyLabel(actLock) != "ctrl+l" {
		t.Errorf("two actions on one key were taken: %s %s", keyLabel(actSave), keyLabel(actLock))
	}
}

func TestEveryActionHasALevelAndHelpInBothLanguages(t *testing.T) {
	for _, d := range keyTable {
		if d.Level == "" || len(d.DefaultKeys) == 0 {
			t.Errorf("action %q has no level or key", d.ID)
		}
		if textEN[textID(d.Help)] == "" || textDE[textID(d.Help)] == "" {
			t.Errorf("action %q lacks a help text in a language", d.ID)
		}
	}
}

// Check refuses exactly the keys the table gives to actions that cannot be replaced, whatever the other
// rules say, and accepts a free key.
func TestCheckRefusesTheKeysOfTheTable(t *testing.T) {
	keepKeys(t)
	for _, d := range keyTable {
		if d.Rebind == kit.RebindReplace {
			continue
		}
		for _, k := range d.DefaultKeys {
			for _, name := range uiprefs.Actions {
				// A table space is spelled "space", the rules know it as " ".
				if k == "space" {
					k = " "
				}
				if k == " " && name == uiprefs.ActionInfo {
					// Normalize trims a space to "", which the info action takes as "no second key".
					continue
				}
				if _, err := uiprefs.DefaultKeys().Check(name, k); err == nil {
					t.Errorf("Check(%s, %q) accepts the key of %s", name, k, d.ID)
				}
			}
		}
	}
	if _, err := uiprefs.DefaultKeys().Check(uiprefs.ActionHelp, "ctrl+x"); err != nil {
		t.Errorf("a free key is refused: %v", err)
	}
	// The table itself, not a copy of the rules, decides: a key it newly gives away is refused.
	rebind(t, actQuit, "ctrl+x")
	if _, err := uiprefs.DefaultKeys().Check(uiprefs.ActionHelp, "ctrl+x"); err == nil || !strings.Contains(err.Error(), string(actQuit)) {
		t.Errorf("Check does not consult the table: %v", err)
	}
	// A key of a replaceable action stays free for another one to take when it is not in the form.
	if owner := reservedOwner("f2"); owner != "" {
		t.Errorf("f2 is reserved by %s, but save can be replaced", owner)
	}
}

// keyLiteral finds the lines that spell a key value: a case item that is a string literal, or a comparison
// of a String() with one.
var keyLiteral = regexp.MustCompile(`^\s*case\s+(?:(?:"[^"]*"|[\w.]+\([^()]*\)|[\w.]+)\s*,\s*)*"[^"]*"\s*[,:]|\.String\(\)\s*[!=]=\s*"`)

// tableKeys are the key names the keymap owns: every default key of every action. No file but keymap.go
// spells one as a key value.
func tableKeys() []string {
	var keys []string
	for _, d := range keyTable {
		for _, k := range d.DefaultKeys {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// Key values live in the keymap: no file outside it switches on a string that could be a key value.
func TestKeyLiteralsStayInTheKeymap(t *testing.T) {
	for _, f := range keyLiteralFindings(t, "*.go") {
		t.Errorf("%s", f)
	}
}

// The keys of the table have no literal anywhere: a case item or a comparison that spells one fails.
func TestTableKeysAreNeverLiterals(t *testing.T) {
	for _, f := range tableKeyFindings(t, "*.go") {
		t.Errorf("%s", f)
	}
}

// tableKeyFindings lists the lines of the files matching glob that spell a key of the table as a literal.
func tableKeyFindings(t *testing.T, glob string) []string {
	t.Helper()
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	type literal struct {
		key string
		re  *regexp.Regexp
	}
	var literals []literal
	for _, k := range tableKeys() {
		q := regexp.QuoteMeta(`"` + k + `"`)
		item := `^\s*case\s+(?:(?:"[^"]*"|[\w.]+\([^()]*\)|[\w.]+)\s*,\s*)*` + q + `\s*[,:]`
		literals = append(literals, literal{k, regexp.MustCompile(item + `|[!=]=\s*` + q + `|` + q + `\s*[!=]=`)})
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "keymap.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, l := range literals {
				if l.re.MatchString(line) {
					found = append(found, f+":"+strconv.Itoa(i+1)+" spells the table key "+l.key+"; use the keymap: "+strings.TrimSpace(line))
				}
			}
		}
	}
	return found
}

// keyLiteralFindings lists the key literals of the files matching glob.
func keyLiteralFindings(t *testing.T, glob string) []string {
	t.Helper()
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "keymap.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if keyLiteral.MatchString(line) {
				found = append(found, f+":"+strconv.Itoa(i+1)+" spells a key value; use the keymap: "+strings.TrimSpace(line))
			}
		}
	}
	return found
}

// The vim letters move only where nothing is typed: the movement bindings do not match them, and moveKey
// leaves them alone on a screen that reads text.
func TestVimLettersMoveOnlyWhereNothingIsTyped(t *testing.T) {
	k := tea.KeyPressMsg{Code: 'k', Text: "k"}
	if keyIs(k, actUp) {
		t.Fatal("k matches the movement binding up")
	}
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.screen = screenCommand
	if !m.typing() {
		t.Fatal("the command line does not read text")
	}
	if got := m.moveKey(k); keyIs(got, actUp) || got.String() != "k" {
		t.Errorf("k became %q on a text screen", got.String())
	}
	m.screen = screenList
	if m.typing() {
		t.Fatal("the list reads text")
	}
	if got := m.moveKey(k); !keyIs(got, actUp) || got.String() != "up" {
		t.Errorf("k became %q on a screen without text, want up", got.String())
	}
}

// The shortcut column of the command line shows the binding in force, also after a rebinding, and the
// descriptions name no key.
func TestPaletteShortcutsFollowTheKeymap(t *testing.T) {
	rebind(t, actQuit, "x")
	rebind(t, actSetup, "ctrl+x")
	m := paletteModel(t, 150, 42)
	m.prefs.Icons = uiprefs.IconsNone
	view := ansi.Strip(m.render())
	for _, name := range []string{"Start the guided setup", "Open Connections"} {
		line := lineWith(view, name)
		if line == "" {
			t.Fatalf("palette lacks %q:\n%s", name, view)
		}
		if strings.Contains(line, "(") {
			t.Errorf("the description names a key: %q", line)
		}
	}
	if line := lineWith(view, "Start the guided setup"); !shortcutIs(line, "ctrl+x") {
		t.Errorf("the setup shortcut is not the bound key: %q", line)
	}
	if line := lineWith(view, "Open Connections"); !shortcutIs(line, "3") {
		t.Errorf("the area shortcut is not its digit: %q", line)
	}
	typeText(t, m, "quit")
	if line := lineWith(ansi.Strip(m.render()), "Leave the editor"); !shortcutIs(line, "x") || strings.Contains(line, "(") {
		t.Errorf("the quit row = %q", line)
	}
	if !strings.Contains(view, ": search or run a command…") {
		t.Errorf("the input line does not start with the colon:\n%s", view)
	}
}

func lineWith(view, text string) string {
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, text) {
			return l
		}
	}
	return ""
}

// A footer names every fixed key through the table: a rebound key, an arrow and a digit range show as bound.
func TestFooterNamesFixedKeysFromTheTable(t *testing.T) {
	rebind(t, actQuit, "x")
	rebind(t, actNew, "N")
	rebind(t, actUp, "ctrl+x")
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.screen, m.section, m.navSub = screenNav, sectionOverview, false
	foot := flatText(m.render())
	for _, want := range []string{"x quit", "1-6 open"} {
		if !strings.Contains(foot, want) {
			t.Errorf("the footer lacks %q:\n%s", want, foot)
		}
	}
	if strings.Contains(foot, "q quit") {
		t.Errorf("the footer still names the old quit key:\n%s", foot)
	}
	if got := keyLabel(actUp) + "/" + keyLabel(actDown); got != "ctrl+x/↓" {
		t.Errorf("label = %q", got)
	}
	if got := footerKeys("{up}/{down} move · {area} open · {pageup}/{pagedown} page"); got != "ctrl+x/↓ move · 1-6 open · pgup/pgdown page" {
		t.Errorf("footerKeys = %q", got)
	}
	if got := keyLabel(actLeft) + keyLabel(actRight); got != "←→" {
		t.Errorf("arrows = %q", got)
	}
}

// The header names help and the command line through the binding in force.
func TestHeaderNamesTheBoundCommandKey(t *testing.T) {
	rebind(t, actCommand, "f3")
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if head := ansi.Strip(m.headerHints()); !strings.Contains(head, "[F3] cmd") || strings.Contains(head, "[:]") {
		t.Errorf("header = %q", head)
	}
}

// Every key a click on a footer group may send is read from the same names the footer draws.
func TestFooterButtonsReadKeyNamesFromTheTable(t *testing.T) {
	for name, code := range map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "F2": tea.KeyF2, "F8": tea.KeyF8} {
		if k, ok := keyPress(name); !ok || k.Code != code {
			t.Errorf("keyPress(%q) = %v, %v", name, k, ok)
		}
	}
	for _, name := range []string{"↑", "←", "1", "up", "F13", "1-6"} {
		if _, ok := keyPress(name); ok {
			t.Errorf("keyPress(%q) is a button key", name)
		}
	}
}

// shortcutIs reports whether the shortcut column of a palette row, the last cell before the frame, is key.
func shortcutIs(line, key string) bool {
	fields := strings.Fields(line)
	return len(fields) >= 2 && fields[len(fields)-2] == key
}
