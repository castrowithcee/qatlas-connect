package tui

import (
	"slices"
	"strings"
	"testing"

	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// keyLineScreens are the screens whose key line is checked: the lists and pages of every area, a form walked
// over its row kinds, the questions, the help and the command line.
func keyLineScreens() []struct {
	name  string
	build func(t *testing.T) *Model
} {
	type screenCase = struct {
		name  string
		build func(t *testing.T) *Model
	}
	var cases []screenCase
	for _, c := range clickScreens() {
		cases = append(cases, screenCase{c.name, c.build})
	}
	for _, c := range clickDialogs() {
		cases = append(cases, screenCase{c.name, c.build})
	}
	for a := area(0); a < areaCount; a++ {
		for _, s := range areaSections[a] {
			cases = append(cases, screenCase{"section " + s.key(), func(t *testing.T) *Model {
				m := sizedModel(groupModel(t), 140, 40)
				openSectionByName(t, m, s)
				return m
			}})
		}
	}
	for _, steps := range [][]string{{"tab"}, {"tab", "tab"}, {"tab", "tab", "tab", "tab"}, {"enter"}} {
		cases = append(cases, screenCase{"connection form " + strings.Join(steps, " "), func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionConnections)
			pressNew(t, m)
			press(t, m, steps...)
			return m
		}})
	}
	cases = append(cases, screenCase{"leave question", func(t *testing.T) *Model {
		m := sizedModel(groupModel(t), 140, 40)
		openSectionByName(t, m, sectionConnections)
		pressNew(t, m)
		press(t, m, "x", "esc")
		if m.screen != screenLeave {
			t.Fatalf("screen %v, want the leave question", m.screen)
		}
		return m
	}})
	return cases
}

// drawnKeyLine is the key text the frame drawn last took for the footer of the screen.
func drawnKeyLine(m *Model) string {
	m.render()
	return m.screenKeys
}

// A screen knows its key line before it draws, as kit hints that carry the groups of the drawn line.
func TestEveryScreenHasItsKeyLineAsHintsBeforeDrawing(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{{"roomy", 140, 40}, {"narrow", 100, 19}} {
		for _, screen := range keyLineScreens() {
			eachLanguage(t, func(t *testing.T) {
				m := sizedModel(screen.build(t), size.w, size.h)
				text := m.screenKeyText()
				hints := m.keyHints(text)
				drawn := drawnKeyLine(m)
				if text != drawn {
					t.Fatalf("%s %s: the key text before drawing is %q, the drawn screen records %q", size.name, screen.name, text, drawn)
				}
				if len(hints) == 0 {
					t.Fatalf("%s %s: no hints for %q", size.name, screen.name, text)
				}
				assertSameGroups(t, m, hints, drawn)
			})
		}
	}
}

// assertSameGroups requires the groups the kit resolves from hints to be those of the drawn key text: each
// key of a hint group stands before the label of the group in some drawn group, and every drawn group is
// made of keys of the hint groups of its label. The kit folds groups that share a label into one, so a
// drawn pair "down open" and "1-6 open" meets the hint group "down/1-6 open".
func assertSameGroups(t *testing.T, m *Model, hints []kit.Hint, drawn string) {
	t.Helper()
	groups := keymap.Hints(hints, kit.InputNavigation, translator(infoExtraLabel() != "" && !m.infoReachable()))
	var lines []string
	for _, g := range strings.Split(m.footerKeys(drawn), " · ") {
		if g != "" && !slices.Contains(lines, g) {
			lines = append(lines, g)
		}
	}
	for _, g := range groups {
		for _, k := range g.Keys {
			if !slices.ContainsFunc(lines, func(line string) bool {
				return strings.HasSuffix(line, g.Label) && strings.Contains(line, k.Keys)
			}) {
				t.Errorf("the key %q of the hint group %q is in no group of the drawn line %q", k.Keys, g.Text(), lines)
			}
		}
	}
	for _, line := range lines {
		// The label of a drawn group is the longest label of the hint groups that ends it.
		best := -1
		for i, g := range groups {
			if strings.HasSuffix(line, g.Label) && (best < 0 || len(g.Label) > len(groups[best].Label)) {
				best = i
			}
		}
		if best < 0 {
			t.Errorf("the drawn group %q has no hint group in %v", line, groups)
			continue
		}
		label := groups[best].Label
		var keys []string
		for _, g := range groups {
			if g.Label == label {
				for _, k := range g.Keys {
					keys = append(keys, k.Keys)
				}
			}
		}
		rest := strings.TrimSuffix(line, label)
		slices.SortFunc(keys, func(a, b string) int { return len(b) - len(a) })
		for _, k := range keys {
			rest = strings.ReplaceAll(rest, k, "")
		}
		for _, word := range []string{" oder ", " or "} {
			rest = strings.ReplaceAll(rest, word, "")
		}
		if strings.Trim(rest, "/, ") != "" {
			t.Errorf("the drawn group %q holds %q that no hint of its label names", line, rest)
		}
	}
}

// The areas and tabs handed to the kit are those the tab line draws.
func TestAreasAndTabsAreKitAreasAndTabs(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		m := sizedModel(groupModel(t), 140, 40)
		areas := m.kitAreas()
		if err := kit.CheckAreas(areas); err != nil {
			t.Fatal(err)
		}
		if len(areas) != int(areaCount) {
			t.Fatalf("%d areas, want %d", len(areas), areaCount)
		}
		tr := translator(false)
		for i, ka := range areas {
			a := area(i)
			if got := tr.Lookup(ka.Title); got != a.title() {
				t.Errorf("area %d is named %q, the tab line draws %q", i+1, got, a.title())
			}
			for j, tab := range ka.Tabs {
				s := areaSections[a][j]
				if got := tr.Lookup(tab.Title); got != s.title() || string(tab.ID) != s.key() {
					t.Errorf("tab %q of %q is %q, the sub-tab line draws %q", tab.ID, ka.ID, got, s.title())
				}
				if tab.GroupStart != (s == advancedStart && j > 0) {
					t.Errorf("tab %q starts a group = %v", tab.ID, tab.GroupStart)
				}
			}
		}
		openSectionByName(t, m, sectionVault)
		active, tab, mode := m.kitActive()
		if active != "Settings" || tab != "Vault" || mode != "" {
			t.Errorf("active %q %q mode %q", active, tab, mode)
		}
	})
}

// The status marks handed to the kit carry the words and the kind the header draws, and the mark about the
// vault stands ahead of the keys.
func TestHeaderMarksAreKitStatusMarks(t *testing.T) {
	m := sizedModel(groupModel(t), 140, 40)
	marks, data := m.kitMarks(), m.vaultMarks()
	if len(marks) != len(data) {
		t.Fatalf("%d kit marks for %d header marks", len(marks), len(data))
	}
	for i, d := range data {
		if marks[i].Text != d.text || (marks[i].Rank == kit.MarkBeforeKeys) != d.urgent {
			t.Errorf("mark %d = %+v, header mark %+v", i, marks[i], d)
		}
		if drawn := m.vaultSegments()[i]; !strings.HasSuffix(drawn, d.text) {
			t.Errorf("the header draws %q for the words %q", drawn, d.text)
		}
	}
}
