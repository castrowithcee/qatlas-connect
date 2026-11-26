package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func commandNames(list []command) []string {
	var names []string
	for _, c := range list {
		names = append(names, c.name)
	}
	return names
}

func cmdlineOpen(m *Model) bool { return m.commandLine() != nil }

// matchNames are the names of the commands the command line of m lists for query, the line left as it was.
func matchNames(t *testing.T, m *Model, query string) []string {
	t.Helper()
	m.openCommandLine()
	defer m.closeModal()
	m.commandLine().Paste(query)
	var names []string
	for _, c := range m.commandLine().Matches() {
		names = append(names, c.Name)
	}
	return names
}

// unavailableOn is why c cannot run with screen s under the command line, "" where it can.
func unavailableOn(m *Model, c command, s screen) string {
	saved := m.screen
	defer func() { m.screen = saved }()
	m.screen = s
	return c.unavailable(m)
}

func cmdModel(t *testing.T) *Model {
	t.Helper()
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestColonOpensTheCommandLineAndEscClosesIt(t *testing.T) {
	m := cmdModel(t)
	seedServices(t, m, "a-one")
	for name, screens := range map[string]func(){
		"tab line": func() { m.screen = screenNav },
		"list":     func() { m.screen = screenList },
		"logs":     func() { openSectionByName(t, m, sectionLogs) },
	} {
		screens()
		from := m.screen
		press(t, m, ":")
		if m.screen != from || !cmdlineOpen(m) || !strings.Contains(screenOf(m), "Commands") {
			t.Fatalf("%s: : opened screen %v:\n%s", name, m.screen, screenOf(m))
		}
		press(t, m, "esc")
		if m.screen != from || cmdlineOpen(m) {
			t.Fatalf("%s: esc = screen %v, want %v", name, m.screen, from)
		}
	}
}

func TestColonStaysTextWhereTextIsTyped(t *testing.T) {
	m := cmdModel(t)
	seedServices(t, m, "a-one")
	m.screen = screenList
	press(t, m, "/", ":")
	if m.screen != screenList || m.list.Query() != ":" {
		t.Fatalf(": in the filter: screen %v query %q, want it as text", m.screen, m.list.Query())
	}
	press(t, m, "esc", "esc")

	addService(t, m, "wiki", "https://wiki.example.invalid")
	openSectionByName(t, m, sectionServices)
	pressNew(t, m)
	typeText(t, m, "a:b")
	if m.screen != screenForm || m.fieldValue("name") != "a:b" {
		t.Fatalf(": in a text row: screen %v name %q", m.screen, m.fieldValue("name"))
	}
}

// A letter on the command line is text, also one that is a key elsewhere.
func TestLettersOnTheCommandLineAreText(t *testing.T) {
	m := cmdModel(t)
	m.screen = screenNav
	press(t, m, ":", "k", "q", "?", "i", "j")
	if !cmdlineOpen(m) || m.quitting || helpOpen(m) || m.commandLine().Query() != "kq?ij" {
		t.Fatalf("letters on the command line: open %v quitting %v help %v query %q", cmdlineOpen(m), m.quitting,
			helpOpen(m), m.commandLine().Query())
	}
}

// A paste goes whole into the line.
func TestPasteGoesIntoTheCommandLine(t *testing.T) {
	m := cmdModel(t)
	m.screen = screenNav
	press(t, m, ":")
	m.Update(tea.PasteMsg{Content: "go logs\n"})
	if got := m.commandLine().Query(); got != "go logs" {
		t.Fatalf("pasted query %q, want %q", got, "go logs")
	}
}

func TestSlashStillFiltersTheList(t *testing.T) {
	m := cmdModel(t)
	seedServices(t, m, "a-one", "b-two")
	m.screen = screenList
	press(t, m, "/")
	typeText(t, m, "two")
	if !m.list.Searching() || m.list.Query() != "two" {
		t.Fatalf("/ = editing %v query %q, want the list filter", m.list.Searching(), m.list.Query())
	}
}

func TestCommandSuggestionsMatchPrefixWordAliasAndCase(t *testing.T) {
	m := cmdModel(t)
	cases := []struct {
		query string
		first string
		has   string
	}{
		{"", "go overview", "quit"},
		// The exact name comes first, before a name that begins with it.
		{"go logs", "go logs", ""},
		{"update", "update", ""},
		{"QU", "quit", ""},
		{"go lo", "go logs", ""},
		{"tokens", "go tokens", ""},
		{"exit", "quit", ""},
		{"vault", "go vault", "lock vault"},
		{"cred", "go credentials", ""},
		{"guided", "setup", ""},
	}
	for _, c := range cases {
		got := matchNames(t, m, c.query)
		if len(got) == 0 || got[0] != c.first {
			t.Errorf("%q matches %v, want %q first", c.query, got, c.first)
		}
		if c.has != "" && !strings.Contains(strings.Join(got, "|"), c.has) {
			t.Errorf("%q matches %v, want %q among them", c.query, got, c.has)
		}
	}
	if got := matchNames(t, m, "zzz"); len(got) != 0 {
		t.Errorf("zzz matches %v, want none", got)
	}
}

func TestEnterRunsAreaCommandsLikeTheDigits(t *testing.T) {
	for a := area(0); a < areaCount; a++ {
		x, y := cmdModel(t), cmdModel(t)
		x.screen, y.screen = screenNav, screenNav
		pump(t, x, string(rune('1'+a)))
		pump(t, y, ":")
		typeText(t, y, "go "+strings.ToLower(a.title()))
		pump(t, y, "enter")
		if y.section != x.section || y.screen != x.screen || cmdlineOpen(y) {
			t.Errorf("%v: command = section %v screen %v, key = section %v screen %v",
				a.title(), y.section, y.screen, x.section, x.screen)
		}
	}
}

// The names the sections had before the areas still open them, inside their area.
func TestEnterRunsSectionAliasesLikeTheOptionBar(t *testing.T) {
	for s := section(0); s < sectionCount; s++ {
		x, y := cmdModel(t), cmdModel(t)
		openSectionByName(t, x, s)
		y.screen = screenNav
		pump(t, y, ":")
		typeText(t, y, "go "+strings.ToLower(s.title()))
		pump(t, y, "enter")
		if y.section != s || y.screen != x.screen || cmdlineOpen(y) {
			t.Errorf("%v: command = section %v screen %v, option bar = section %v screen %v",
				s.title(), y.section, y.screen, x.section, x.screen)
		}
	}
}

// The empty line lists the areas, not every sub-tab.
func TestEmptyCommandLineListsOnlyTheAreas(t *testing.T) {
	var got []string
	for _, name := range matchNames(t, cmdModel(t), "") {
		if strings.HasPrefix(name, "go ") {
			got = append(got, name)
		}
	}
	want := "go overview|go library|go connections|go activity|go access|go settings"
	if strings.Join(got, "|") != want {
		t.Errorf("go commands = %v, want %s", got, want)
	}
}

func TestEnterRunsSetupHelpAndQuit(t *testing.T) {
	a, b := cmdModel(t), cmdModel(t)
	pump(t, a, "g")
	pump(t, b, ":")
	typeText(t, b, "setup")
	pump(t, b, "enter")
	if b.screen != a.screen || b.wizard == nil != (a.wizard == nil) {
		t.Errorf("setup: screen %v wizard %v, key: screen %v wizard %v", b.screen, b.wizard != nil, a.screen, a.wizard != nil)
	}

	m := cmdModel(t)
	pump(t, m, ":")
	typeText(t, m, "help")
	pump(t, m, "enter")
	if !helpOpen(m) || m.screen != screenNav {
		t.Errorf("help: open %v over screen %v", helpOpen(m), m.screen)
	}

	m = cmdModel(t)
	pump(t, m, ":")
	typeText(t, m, "quit")
	_, cmd := m.Update(keyMsg("enter"))
	if !m.quitting || cmd == nil {
		t.Errorf("quit: quitting %v", m.quitting)
	}
}

func TestEnterRunsUpdateLikeU(t *testing.T) {
	m := startedWith(t, newerRelease())
	pump(t, m, ":")
	typeText(t, m, "update")
	pump(t, m, "enter")
	if m.screen != screenUpdate || !strings.Contains(screenOf(m), "Update qconnect to v0.5.0?") {
		t.Fatalf("update = screen %v:\n%s", m.screen, screenOf(m))
	}
}

func TestEnterRunsLockVaultLikeCtrlL(t *testing.T) {
	m, _ := newEncryptedVaultModel(t, filepath.Join(t.TempDir(), "qconnect"), "hunter2", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	openSectionByName(t, m, sectionServices)
	pump(t, m, ":")
	typeText(t, m, "lock vault")
	pump(t, m, "enter")
	if m.screen != screenConfirm || !m.vaultLockConfirm {
		t.Fatalf("lock vault = screen %v confirm %v, want the lock question", m.screen, m.vaultLockConfirm)
	}
}

func TestEscClosesTheCommandLineWithoutEffect(t *testing.T) {
	m := cmdModel(t)
	m.screen = screenNav
	pump(t, m, ":")
	typeText(t, m, "go logs")
	pump(t, m, "esc")
	if m.screen != screenNav || m.section != sectionServices || m.quitting {
		t.Fatalf("esc = screen %v section %v", m.screen, m.section)
	}
}

func TestUnknownAndImpossibleCommandsSayWhy(t *testing.T) {
	m := cmdModel(t)
	pump(t, m, ":")
	typeText(t, m, "frobnicate")
	pump(t, m, "enter")
	if !cmdlineOpen(m) || !strings.Contains(screenOf(m), `unknown command "frobnicate"`) {
		t.Fatalf("unknown command:\n%s", screenOf(m))
	}
	press(t, m, "esc")

	// No vault in this model: the command is refused with its reason and the line stays open.
	pump(t, m, ":")
	typeText(t, m, "lock vault")
	pump(t, m, "enter")
	if !cmdlineOpen(m) || !strings.Contains(screenOf(m), "lock vault: ") {
		t.Fatalf("impossible command:\n%s", screenOf(m))
	}
	press(t, m, "esc")

	// No update known.
	pump(t, m, ":")
	typeText(t, m, "update")
	pump(t, m, "enter")
	if !cmdlineOpen(m) || !strings.Contains(screenOf(m), "update: ") {
		t.Fatalf("update without a release:\n%s", screenOf(m))
	}
}

func TestSectionCommandIsRefusedOnAConfirmationAndKeepsIt(t *testing.T) {
	m := cmdModel(t)
	addService(t, m, "wiki", "https://wiki.example.invalid")
	openSectionByName(t, m, sectionServices)
	press(t, m, "d")
	if m.screen != screenConfirm {
		t.Fatalf("d = screen %v, want the confirmation", m.screen)
	}
	press(t, m, ":")
	typeText(t, m, "go logs")
	pump(t, m, "enter")
	if !cmdlineOpen(m) || !strings.Contains(screenOf(m), "cannot switch areas") {
		t.Fatalf("refusal:\n%s", screenOf(m))
	}
	press(t, m, "esc")
	if m.screen != screenConfirm || m.section != sectionServices {
		t.Fatalf("esc = screen %v section %v, want the confirmation unchanged", m.screen, m.section)
	}
}

func TestEveryFooterWithoutTextEntryNamesTheCommandLine(t *testing.T) {
	m := cmdModel(t)
	seedServices(t, m, "a-one")
	for _, s := range []screen{screenNav, screenList} {
		m.screen = s
		if flat := strings.Join(strings.Fields(screenOf(m)), " "); !strings.Contains(flat, ": cmds") {
			t.Errorf("screen %v footer lacks ': cmds':\n%s", s, screenOf(m))
		}
	}
}

func paletteModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.screen = screenNav
	press(t, m, ":")
	return m
}

// The command line stands at the top of the screen under the kit's title, with the placeholder, the key of
// each command at the right and a key line of enter and back.
func TestPaletteIsAnOverlayAtTheTopWithPlaceholderAndShortcuts(t *testing.T) {
	useColour(t)
	m := paletteModel(t, 150, 42)
	view := ansi.Strip(m.render())
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "╭") || !strings.Contains(lines[0], "Commands") {
		t.Fatalf("the command line does not stand at the top under its title:\n%s", view)
	}
	for _, want := range []string{": search or run a command…", "Open Connections", "open or confirm", "back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the command line lacks %q:\n%s", want, view)
		}
	}
	for _, l := range lines {
		if strings.Contains(l, "Open Connections") && !strings.HasSuffix(strings.TrimRight(l, " │"), "3") {
			t.Errorf("shortcut not at the right: %q", l)
		}
	}
	if len(lines) != 42 || !strings.Contains(view, "Qatlas") {
		t.Errorf("the screen below is gone: %d lines", len(lines))
	}
	press(t, m, "c", "o", "n")
	view = ansi.Strip(m.render())
	if strings.Contains(view, "search or run a command") || !strings.Contains(view, ": con") {
		t.Errorf("typed text not in the field:\n%s", view)
	}
}

// A list longer than the rows shown says which of them show.
func TestPaletteShowsItsPositionWhereNotAllMatchesShow(t *testing.T) {
	m := paletteModel(t, 150, 42)
	n := len(m.commandLine().Matches())
	if n <= 12 {
		t.Fatalf("%d commands fit the line at once; the test needs more", n)
	}
	if want := fmt.Sprintf("1-12 of %d", n); !strings.Contains(ansi.Strip(m.render()), want) {
		t.Errorf("the command line lacks the position %q:\n%s", want, ansi.Strip(m.render()))
	}
}

func TestPaletteScrollKeepsTheCursorVisible(t *testing.T) {
	m := paletteModel(t, 80, 24)
	matches := m.commandLine().Matches()
	for range len(matches) - 1 {
		press(t, m, "down")
	}
	last := matches[len(matches)-1]
	if !strings.Contains(ansi.Strip(m.render()), last.Name) {
		t.Errorf("cursor row %q not visible:\n%s", last.Name, ansi.Strip(m.render()))
	}
}

// The wheel over the matches moves the choice a match a notch and stops at the first and the last, where the
// arrow keys wrap round.
func TestPaletteWheelMovesTheChoiceAndStopsAtTheEnds(t *testing.T) {
	chosen := func(m *Model) string {
		for _, l := range strings.Split(ansi.Strip(m.render()), "\n") {
			if strings.Contains(l, styles.SelectionMark()+" ") && strings.Contains(l, "go ") {
				return l
			}
		}
		return ""
	}
	m := paletteModel(t, 150, 42)
	x, y := cellOf(t, m, "go library")
	first := chosen(m)
	wheelEvent(t, m, x, y, tea.MouseWheelUp)
	if got := chosen(m); got != first || !strings.Contains(got, "go overview") {
		t.Errorf("a notch up at the first match chose %q, want it to stay", got)
	}
	wheelEvent(t, m, x, y, tea.MouseWheelDown)
	if got := chosen(m); !strings.Contains(got, "go library") || !cmdlineOpen(m) {
		t.Errorf("a notch down chose %q, want go library", got)
	}

}

func TestPaletteClickRunsTheEntry(t *testing.T) {
	m := paletteModel(t, 150, 42)
	clickOn(t, m, "Open Connections")
	if cmdlineOpen(m) || m.section.area() != areaConnections {
		t.Fatalf("click left the palette open %v, section %v", cmdlineOpen(m), m.section)
	}
}

func TestPaletteReadsWithoutColour(t *testing.T) {
	before := currentProfile()
	prior := styles
	t.Cleanup(func() { setProfile(before); setTheme(prior) })
	setProfile(kittui.ProfileNone)
	applyTheme(kittui.ThemeTerminal)
	m := paletteModel(t, 150, 42)
	view := m.render()
	if strings.Contains(view, "\x1b[") {
		t.Errorf("colour sequence in the palette: %q", view)
	}
	if !strings.Contains(view, styles.SelectionMark()) || !strings.Contains(view, "Commands") {
		t.Errorf("selection lost:\n%s", view)
	}
}

// From the smallest frame up to the size of a modal over the screen, the command line stands over the screen
// or fills the workspace, and runs what is typed.
func TestPaletteStaysUsableAtSmallSizes(t *testing.T) {
	for _, size := range []struct{ w, h int }{{minimumWidth, minimumHeight}, {44, 15}, {50, 17}, {59, 19}} {
		m := paletteModel(t, size.w, size.h)
		view := m.render()
		assertViewFits(t, view, size.w, size.h)
		if !cmdlineOpen(m) || !strings.Contains(ansi.Strip(view), ": search") {
			t.Fatalf("%dx%d: no command line:\n%s", size.w, size.h, ansi.Strip(view))
		}
		press(t, m, "h", "e", "l", "p", "enter")
		if !helpOpen(m) {
			t.Errorf("%dx%d: enter did not run help:\n%s", size.w, size.h, ansi.Strip(m.render()))
		}
	}
}

// Over a list of the kit, which takes only its own keys, F9 opens no help, and the command version that opened
// the list leaves it open once the command line closed.
func TestF9DoesNothingOverAKitList(t *testing.T) {
	m := startedVersions(t, choosable())
	openVersions(t, m)
	if !choiceOpen(m, screenVersions) {
		t.Fatalf("the version list is not open: screen %v", m.screen)
	}
	press(t, m, "f9")
	if helpOpen(m) || !choiceOpen(m, screenVersions) {
		t.Errorf("f9 over the version list: help %v list open %v", helpOpen(m), choiceOpen(m, screenVersions))
	}
}

// The command line opened from a question over a form stands over the question, which comes back when it
// closes.
func TestCommandLineFromAQuestionReturnsToIt(t *testing.T) {
	m, _, _ := knownTargetsModel(t)
	openConnection(t, m, "gh-a")
	focusField(t, m, effectsLabel)
	press(t, m, "enter")
	typeText(t, m, "create")
	press(t, m, " ", "esc", "esc")
	if m.screen != screenLeave || m.question == nil {
		t.Fatalf("no leave question: screen %v", m.screen)
	}
	asked := m.question
	press(t, m, keyLabel(actCommand))
	if !cmdlineOpen(m) || m.screen != screenLeave {
		t.Fatalf(": over the question: command line %v screen %v", cmdlineOpen(m), m.screen)
	}
	press(t, m, "esc")
	m.render()
	if m.screen != screenLeave || m.modal != asked {
		t.Errorf("closing the command line left screen %v without the question", m.screen)
	}
}
