package tui

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/helptopics"
	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kithelp "github.com/castrowithcee/qatlas-kit/helptopics"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// helpOf is the help open over the screen, nil for none.
func helpOf(m *Model) *kittui.Help {
	h, _ := m.modal.(vimHelp)
	return h.Help
}

func helpOpen(m *Model) bool { return helpOf(m) != nil }

// helpTopicIndex is the index of the topic the open help shows.
func helpTopicIndex(t *testing.T, m *Model) int {
	t.Helper()
	if !helpOpen(m) {
		t.Fatal("the help is not open")
	}
	return slices.IndexFunc(helptopics.All(), func(topic kithelp.Topic) bool { return topic.Name == helpOf(m).Topic() })
}

var helpPosition = regexp.MustCompile(`lines (\d+)-(\d+) of (\d+)`)

// helpScroll is the first shown line of the help text, counted from 0, and whether its last line is in view,
// as the position line of the drawn help says; a text without that line fits.
func helpScroll(t *testing.T, m *Model) (offset int, atEnd bool) {
	t.Helper()
	found := helpPosition.FindAllStringSubmatch(ansi.Strip(m.render()), -1)
	if len(found) == 0 {
		return 0, true
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	last := found[len(found)-1]
	return n(last[1]) - 1, n(last[2]) == n(last[3])
}

func helpOffset(t *testing.T, m *Model) int {
	t.Helper()
	offset, _ := helpScroll(t, m)
	return offset
}

// firstWords is the start of a topic's text, short enough to stand on one line of any workspace.
func firstWords(topic kithelp.Topic) string {
	return strings.Join(strings.Fields(topic.Text)[:3], " ")
}

// ? opens the help from the tab line and from the list, every topic is one key away, and esc returns to where
// it was opened.
func TestHelpShowsEveryTopicAndClosesWithEscape(t *testing.T) {
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	if keys := strings.Join(strings.Fields(screenOf(m)), " "); !strings.Contains(keys, ": cmds") || !namesHelp(keys) {
		t.Errorf("the tab line keys do not offer ? help:\n%s", screenOf(m))
	}

	press(t, m, "?")
	if !helpOpen(m) || m.screen != screenNav {
		t.Fatalf("? on the tab line: help %v over screen %v", helpOpen(m), m.screen)
	}
	topics := helptopics.All()
	for i, key := range []string{"right", "right", "right", "right"} {
		topic := topics[i%len(topics)]
		view := m.render()
		if !strings.Contains(view, "["+topic.Name+"]") || !strings.Contains(view, firstWords(topic)) {
			t.Errorf("topic %s is not shown:\n%s", topic.Name, view)
		}
		if !strings.Contains(view, " Help ") {
			t.Errorf("the help is not titled Help:\n%s", view)
		}
		press(t, m, key)
	}
	if i := helpTopicIndex(t, m); i != 0 {
		t.Errorf("right after the last topic shows topic %d, want the first", i)
	}
	press(t, m, "left")
	if last := topics[len(topics)-1]; !strings.Contains(screenOf(m), "["+last.Name+"]") {
		t.Errorf("left from the first topic does not show the last one:\n%s", screenOf(m))
	}
	press(t, m, "tab")
	if i := helpTopicIndex(t, m); i != 0 {
		t.Errorf("tab does not switch the topic: %d", i)
	}
	press(t, m, "esc")
	if helpOpen(m) || m.screen != screenNav {
		t.Fatalf("esc returned to screen %v, help open %v, want the tab line", m.screen, helpOpen(m))
	}

	press(t, m, "enter")
	if keys := strings.Join(strings.Fields(screenOf(m)), " "); !strings.Contains(keys, ": cmds") || !namesHelp(keys) {
		t.Errorf("the list keys do not offer ? help:\n%s", screenOf(m))
	}
	press(t, m, "?", "right")
	if !helpOpen(m) || !strings.Contains(screenOf(m), "[keys]") {
		t.Fatalf("? in the list does not open the help:\n%s", screenOf(m))
	}
	press(t, m, "esc")
	if helpOpen(m) || m.screen != screenList {
		t.Errorf("esc returned to screen %v, want the list", m.screen)
	}
}

// A topic longer than its room scrolls, over the screen and in the workspace, and the screen keeps its size
// while it does.
func TestHelpScrollsWithinItsRoom(t *testing.T) {
	m, _, _ := newModel(t)
	for _, size := range []struct{ width, height int }{{100, 28}, {60, 24}, {40, minimumHeight}} {
		m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m.screen = screenNav
		press(t, m, "?", "right")
		view := m.render()
		assertViewFits(t, view, size.width, size.height)
		if !strings.Contains(view, "lines 1-") {
			t.Fatalf("%dx%d: the keys topic does not say it scrolls:\n%s", size.width, size.height, view)
		}
		press(t, m, "down", "down")
		if got := helpOffset(t, m); got != 2 {
			t.Errorf("%dx%d: down twice scrolled to %d", size.width, size.height, got)
		}
		press(t, m, "end")
		assertViewFits(t, m.render(), size.width, size.height)
		end, atEnd := helpScroll(t, m)
		if !atEnd {
			t.Errorf("%dx%d: end does not show the last line:\n%s", size.width, size.height, m.render())
		}
		press(t, m, "down")
		if got := helpOffset(t, m); got != end {
			t.Errorf("%dx%d: down scrolled past the end", size.width, size.height)
		}
		press(t, m, "home")
		if got := helpOffset(t, m); got != 0 {
			t.Errorf("%dx%d: home scrolled to %d", size.width, size.height, got)
		}
		press(t, m, "pgdown")
		if helpOffset(t, m) == 0 {
			t.Errorf("%dx%d: pgdown did not scroll", size.width, size.height)
		}
		press(t, m, "right")
		if got := helpOffset(t, m); got != 0 {
			t.Errorf("%dx%d: a new topic does not start at its top", size.width, size.height)
		}
		press(t, m, "esc")
	}
}

// Where keys are text, ? is text: in a form and in the filter of a list.
func TestQuestionMarkIsTextWhereTextIsTyped(t *testing.T) {
	m, _, _ := newModel(t)
	press(t, m, "2", "n")
	typeText(t, m, "a?b")
	if m.screen != screenForm || m.fieldValue("name") != "a?b" {
		t.Errorf("? in a form left it or was not typed: screen %v name %q", m.screen, m.fieldValue("name"))
	}

	list, _, _ := newModel(t)
	press(t, list, "enter", "/", "?")
	if list.screen != screenList || list.list.query() != "?" {
		t.Errorf("? in a filter was not typed: screen %v query %q", list.screen, list.list.query())
	}
}

// The help stands as a framed panel over the screen it was opened from wherever three text rows and an inner
// width of 36 cells fit, and fills the workspace below that; the topics are tabs and the keys are named, also
// without colour, and every size from 44x15 up draws it without error.
func TestHelpIsAPanelOverTheScreenWhereThereIsRoom(t *testing.T) {
	type size struct {
		width, height int
		over          bool
	}
	sizes := []size{{100, 28, true}, {modalMinWidth, modalMinHeight, true}, {modalMinWidth - 1, modalMinHeight, true},
		{43, minimumHeight, false}, {minimumWidth, minimumHeight, false}}
	for w := 44; w <= 59; w++ {
		for h := minimumHeight; h <= 19; h++ {
			sizes = append(sizes, size{w, h, true})
		}
	}
	for _, noColor := range []bool{false, true} {
		restoreRenderer(t)
		t.Setenv("NO_COLOR", "")
		useDarkColours()
		if noColor {
			withNoColor(t)
		}
		m, _, _ := newModel(t)
		for _, size := range sizes {
			m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			m.screen = screenNav
			press(t, m, "?")
			if m.fail != "" {
				t.Fatalf("%dx%d: the help failed: %s", size.width, size.height, m.fail)
			}
			view := m.render()
			assertViewFits(t, view, size.width, size.height)
			plain := ansi.Strip(view)
			framed := strings.Contains(plain, "╭─ Help ") || strings.Contains(plain, "╔═ Help ")
			if framed != size.over {
				t.Errorf("%dx%d: panel over the screen = %v:\n%s", size.width, size.height, framed, plain)
			}
			if !strings.Contains(plain, "["+helptopics.All()[0].Name+"]") || !strings.Contains(plain, "close") {
				t.Errorf("%dx%d: the help lacks its topics or its keys:\n%s", size.width, size.height, plain)
			}
			if noColor && plain != view {
				t.Errorf("%dx%d: NO_COLOR still emits escape sequences", size.width, size.height)
			}
			press(t, m, "esc")
			if helpOpen(m) {
				t.Fatalf("%dx%d: esc left the help open", size.width, size.height)
			}
		}
	}
}

// The key lists stand at the end of the first topic, the keys of the screen under "This screen" and the
// global actions under "Everywhere", with the keys bound now; the other topics end
// without them.
func TestHelpKeyListsEndTheFirstTopic(t *testing.T) {
	keepKeys(t)
	applyKeys(uiprefs.Keys{Command: "f6"})
	m := sizedModel(groupModel(t), 120, 40)
	openSectionByName(t, m, sectionConnections)
	press(t, m, "?")
	// The help learns the length of its text as it draws, as after every key of a run.
	m.render()
	press(t, m, "end")
	plain := ansi.Strip(m.render())
	screen, all := strings.Index(plain, "This screen"), strings.Index(plain, "Everywhere")
	if screen < 0 || screen > all {
		t.Fatalf("the keys of the screen do not come first:\n%s", plain)
	}
	if !strings.Contains(plain[all:], "F6") {
		t.Errorf("the global keys do not name the command key in force:\n%s", plain)
	}
	press(t, m, "right")
	m.render()
	press(t, m, "end")
	if plain := ansi.Strip(m.render()); strings.Contains(plain, "Everywhere") {
		t.Errorf("the second topic ends with the key lists:\n%s", plain)
	}
}

func TestHelpOpensOnTheTopicItLastShowed(t *testing.T) {
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 42})
	press(t, m, "?", "right", "right", "esc", "?")
	if i := helpTopicIndex(t, m); i != 2 {
		t.Fatalf("the help opens on topic %d, want the last shown topic 2", i)
	}
}
