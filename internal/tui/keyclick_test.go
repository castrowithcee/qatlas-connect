package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// clickScreens build the screens whose keys the keymap names: each has a footer with key groups and, one level
// down, the back button. Every call builds a new model, so a key and a click start from the same state.
func clickScreens() []struct {
	name  string
	build func(t *testing.T) *Model
} {
	return []struct {
		name  string
		build func(t *testing.T) *Model
	}{
		{"overview", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			m.screen, m.section, m.navSub = screenNav, sectionOverview, false
			return m
		}},
		{"connections list", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionConnections)
			return m
		}},
		{"credentials list", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionCredentials)
			return m
		}},
		{"connections list filtered", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionConnections)
			press(t, m, "/")
			typeText(t, m, "tracker")
			press(t, m, "enter")
			return m
		}},
		{"form", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionConnections)
			pressNew(t, m)
			return m
		}},
		{"help", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			m.screen = screenNav
			press(t, m, keyLabel(actHelp))
			return m
		}},
		{"command line", func(t *testing.T) *Model { return paletteModel(t, 140, 40) }},
		{"settings page", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionGeneral)
			press(t, m, "enter")
			return m
		}},
	}
}

// state is what a key and a click must leave alike: the screen, the focus, the selection and the view. The
// view names the configuration file, which every model of a test keeps in a directory of its own.
var shownConfigPath = regexp.MustCompile(`\S*config\.yaml`)

func clickState(m *Model) string {
	selected, _ := m.marked()
	view := strings.ReplaceAll(m.render(), filepath.Dir(m.svc.Path()), "<dir>")
	// Each model has its own temporary directory, and a shortened path shows only its end.
	view = shownConfigPath.ReplaceAllString(view, "<config>")
	return fmt.Sprintf("screen %v section %v focus %d sub %v selected %q\n%s", m.screen, m.section, m.focus, m.navSub, selected, view)
}

// clickDialogs are the questions whose answers the key line names: the key line groups and the buttons drawn
// from them are targets. buttons names the key of each button by its label.
func clickDialogs() []struct {
	name    string
	build   func(t *testing.T) *Model
	buttons map[string]string
} {
	return []struct {
		name    string
		build   func(t *testing.T) *Model
		buttons map[string]string
	}{
		{"delete question", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionCredentials)
			press(t, m, keyLabel(actDelete))
			if m.screen != screenConfirm {
				t.Fatalf("screen %v, want the delete question", m.screen)
			}
			return m
		}, map[string]string{"Delete": keyLabel(actYes), "Cancel": keyLabel(actNo)}},
	}
}

// Every control of the frame is a target, and a click in its middle acts as the first key bound to its action,
// with every question and check of the key: the back button and the keys of the header, every key group of
// the footer, and the tabs, whose click on the tab line is the digit of the area. The dialog answers the key
// line names are targets as well. The keys come from the keymap, also after the preferences rebound some.
func TestEveryScreenControlHasATargetAndClicksAsItsKey(t *testing.T) {
	backButtons, footerTargets := 0, 0
	var listTargets listTargetCount
	for _, binding := range []struct {
		name string
		keys map[action]string
	}{{"default keys", nil}, {"rebound keys", map[action]string{actInfo: "f3", actSave: "f4", actHelp: "ctrl+x", actCommand: "f6", actLock: "ctrl+l"}}} {
		for _, screen := range frameScreens() {
			t.Run(binding.name+"/"+screen.name, func(t *testing.T) {
				keepKeys(t)
				applyKeys(binding.keys)
				base := screen.build(t)
				base.render()
				if base.modalShown() {
					if len(base.frameKeys) > 0 {
						t.Fatalf("the frame behind a modal has key targets: %+v", base.frameKeys)
					}
					return
				}
				assertFrameControlsAreTargets(t, base)
				for _, target := range base.frameKeys {
					x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
					if target.Action == actBack && y == 0 {
						backButtons++
					}
					if y > 0 {
						footerTargets++
					}
					assertClickActsAsKey(t, screen.build, x, y, firstKey(t, target.Action))
				}
				listTargets.add(assertListTargetsClickAsKeys(t, base, screen.build))
				if base.screen != screenNav || base.navSub {
					return
				}
				// On the focused tab line the arrows reach a tab as its click does: the area opens there and the
				// line keeps the focus; the sub-tabs lie one line down.
				active := base.section.area()
				for _, target := range base.frameTabs {
					a, _ := areaByName(string(target.Area))
					keys := slices.Repeat([]string{"right"}, (int(a)-int(active)+int(areaCount))%int(areaCount))
					if target.Tab != "" {
						at := slices.IndexFunc(areaSections[a], func(s section) bool { return s.key() == string(target.Tab) })
						keys = append([]string{"down"}, slices.Repeat([]string{"right"}, at-base.section.index())...)
					}
					assertClickActsAsKey(t, screen.build, target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y, keys...)
				}
			})
		}
		for _, dialog := range clickDialogs() {
			t.Run(binding.name+"/"+dialog.name, func(t *testing.T) {
				keepKeys(t)
				applyKeys(binding.keys)
				base := dialog.build(t)
				base.render()
				lines := frameLines(base)
				seen := map[string]bool{}
				if len(base.hits) == 0 {
					t.Fatal("the dialog has no target")
				}
				for _, h := range base.hits {
					text := ansi.Cut(lines[h.y0], h.x0, h.x1)
					key := footerKey(text)
					if label, ok := strings.CutPrefix(strings.TrimSpace(text), "[ "); ok {
						label = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(label), "]"))
						if key = dialog.buttons[label]; key == "" {
							t.Errorf("the button %q is none of %v", label, dialog.buttons)
							continue
						}
						seen[label] = true
					}
					if key == "" {
						t.Errorf("the target %q names no key", text)
						continue
					}
					msg, ok := keyPress(key)
					if !ok {
						t.Fatalf("%q is no key a control may send", key)
					}
					assertClickActsAsKey(t, dialog.build, (h.x0+h.x1-1)/2, (h.y0+h.y1-1)/2, kittui.KeyName(msg))
				}
				for label := range dialog.buttons {
					if !seen[label] {
						t.Errorf("the button %q has no target", label)
					}
				}
			})
		}
	}
	if backButtons == 0 || footerTargets == 0 {
		t.Fatalf("back buttons %d, footer targets %d: the screens drew no such control", backButtons, footerTargets)
	}
	if listTargets.rows == 0 || listTargets.groups == 0 || listTargets.search == 0 || listTargets.free == 0 {
		t.Fatalf("list targets %+v: the lists drew no such target", listTargets)
	}
}

// listTargetCount counts the targets of the lists by kind.
type listTargetCount struct{ rows, groups, search, free int }

func (c *listTargetCount) add(n listTargetCount) {
	c.rows, c.groups, c.search, c.free = c.rows+n.rows, c.groups+n.groups, c.search+n.search, c.free+n.free
}

// assertListTargetsClickAsKeys clicks the middle of every target the list of a screen drew, while its rows
// have the focus: an entry row acts as the moves to it, and on the marked row as enter; a group row as the
// moves to it and enter, which folds it; the search line as the search key; the free area as nothing, since
// the rows have the focus already.
func assertListTargetsClickAsKeys(t *testing.T, base *Model, build func(t *testing.T) *Model) listTargetCount {
	t.Helper()
	var n listTargetCount
	if !base.listSection() || !base.rowsFocused() {
		return n
	}
	rows := listRowsOf(base)
	at, _ := base.marked()
	from := slices.Index(rows, at)
	elements := func(x, y int) bool {
		for _, o := range base.listTargets {
			r := o.Rect
			if (o.Item != "" || o.Action != "") && x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
				return true
			}
		}
		return false
	}
	for _, target := range base.listTargets {
		r := target.Rect
		x, y := r.X+(r.Width-1)/2, r.Y
		var keys []string
		switch {
		case target.Item != "":
			name := target.Item
			if key, ok := strings.CutPrefix(name, "\x00g"); ok {
				name = groupRow(key)
			}
			to := slices.Index(rows, name)
			if to < 0 || from < 0 {
				t.Errorf("the row target %q is no shown row of %q", name, rows)
				continue
			}
			move := "down"
			if to < from {
				move = "up"
			}
			keys = slices.Repeat([]string{move}, max(to-from, from-to))
			if isGroupRow(name) {
				n.groups++
				keys = append(keys, "enter")
			} else {
				n.rows++
				if to == from {
					keys = append(keys, "enter")
				}
			}
		case target.Action == actSearch:
			n.search++
			keys = []string{firstKey(t, actSearch)}
		default:
			for y = r.Y; y < r.Y+r.Height && elements(x, y); y++ {
			}
			if y == r.Y+r.Height {
				continue
			}
			n.free++
		}
		assertClickActsAsKey(t, build, x, y, keys...)
	}
	return n
}

// frameScreens are the screens of the key line test and the questions that fill the workspace of a terminal
// too small for a modal, whose footer holds their answers.
func frameScreens() []struct {
	name  string
	build func(t *testing.T) *Model
} {
	screens := keyLineScreens()
	for _, s := range append([]struct {
		name  string
		build func(t *testing.T) *Model
	}{}, screens...) {
		if s.name == "leave question" || s.name == "delete question" {
			build := s.build
			screens = append(screens, struct {
				name  string
				build func(t *testing.T) *Model
			}{s.name + " in the workspace", func(t *testing.T) *Model { return sizedModel(build(t), modalMinWidth-1, 24) }})
		}
	}
	return screens
}

// firstKey is the name of the first key bound to an action, which a click on its hint stands for.
func firstKey(t *testing.T, a action) string {
	t.Helper()
	keys := keymap.Keys(a)
	if len(keys) == 0 {
		t.Fatalf("the action %q has no key", a)
	}
	return keys[0]
}

// modalShown reports whether the frame drawn last holds a modal over the screen.
func (m *Model) modalShown() bool { d := m.dialogBox; return d.x1 > d.x0 && d.y1 > d.y0 }

// assertFrameControlsAreTargets requires a target for each control the frame draws: the back button, the
// header keys and each key of a footer group with its label, as cells of the drawn lines, and each tab.
func assertFrameControlsAreTargets(t *testing.T, m *Model) {
	t.Helper()
	view, _ := m.kitFrame().Render(styles.Styles, m.spec)
	lines := strings.Split(ansi.Strip(view), "\n")
	covered := func(y int, cells string, at int) bool {
		for _, target := range m.frameKeys {
			r := target.Rect
			if r.Y == y && r.X <= at && at < r.X+r.Width {
				return true
			}
		}
		return false
	}
	text := translator(infoExtraLabel() != "" && !m.infoReachable())
	keys := m.spec.HeaderKeys
	if m.spec.Back != nil {
		keys = append(keys, *m.spec.Back)
	}
	for _, g := range keymap.Hints(keys, m.spec.Input, text) {
		block := "[" + g.Keys[0].Keys + "] " + g.Label
		if at := strings.Index(lines[0], block); at >= 0 && !covered(0, block, ansi.StringWidth(lines[0][:at])) {
			t.Errorf("the header key %q has no target:\n%s", block, lines[0])
		}
	}
	for y := m.top + m.height; y < len(lines); y++ {
		for _, g := range keymap.Hints(m.spec.Footer, m.spec.Input, text) {
			at := strings.Index(lines[y], g.Text())
			if at < 0 || len(g.Keys) == 0 {
				continue
			}
			x := ansi.StringWidth(lines[y][:at])
			if !covered(y, g.Text(), x) || !covered(y, g.Text(), x+ansi.StringWidth(g.Text())-1) {
				t.Errorf("the footer group %q has no target:\n%s", g.Text(), lines[y])
			}
		}
	}
	tabs := 0
	for _, target := range m.frameTabs {
		if target.Tab == "" {
			tabs++
		}
	}
	if tabs != int(areaCount) {
		t.Errorf("%d of %d area tabs are targets:\n%s", tabs, areaCount, lines[1])
	}
}

// assertClickActsAsKey builds two equal screens, presses the keys on one and clicks the cell on the other, and
// requires the same screen, focus, selection and view.
func assertClickActsAsKey(t *testing.T, build func(t *testing.T) *Model, x, y int, keys ...string) {
	t.Helper()
	pressed, clicked := build(t), build(t)
	pressed.render()
	clicked.render()
	for _, key := range keys {
		_, cmd := pressed.Update(keyMsg(key))
		deliver(pressed, cmd)
	}
	clickAt(t, clicked, x, y)
	if want, got := clickState(pressed), clickState(clicked); want != got {
		t.Errorf("a click at (%d,%d) does not act as the keys %q:\nkey:\n%s\nclick:\n%s", x, y, keys, want, got)
	}
}
