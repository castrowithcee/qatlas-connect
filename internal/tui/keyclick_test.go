package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
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

// state is what a key and a click must leave alike: the screen, the focus, the selection and the view.
func clickState(m *Model) string {
	selected, _ := m.list.selected()
	return fmt.Sprintf("screen %v section %v focus %d selected %q\n%s", m.screen, m.section, m.focus, selected, m.render())
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

// Every key a footer names and every back button and dialog answer a screen draws as a control is a target, and
// a click in its middle acts as the key it names, with every question and check of the key. The keys come from
// the keymap, also after the preferences rebound some. The key groups of the footer of a plain screen are no
// targets yet; they become ones with the frame of the kit.
func TestEveryScreenControlHasATargetAndClicksAsItsKey(t *testing.T) {
	backButtons := 0
	for _, binding := range []struct {
		name string
		keys uiprefs.Keys
	}{{"default keys", uiprefs.DefaultKeys()}, {"rebound keys", uiprefs.Keys{Info: "f3", Save: "f4", Help: "ctrl+x", Command: "f6", Lock: "ctrl+l"}}} {
		for _, screen := range clickScreens() {
			t.Run(binding.name+"/"+screen.name, func(t *testing.T) {
				keepKeys(t)
				applyKeys(binding.keys)
				base := screen.build(t)
				base.render()
				_, back := base.headerLayout()
				if back.x1 <= back.x0 {
					return // the screen is one level deep: esc leads nowhere, so there is no button
				}
				if !base.hitAt(back.x0, 0) {
					t.Fatalf("the back button has no target:\n%s", ansi.Strip(base.render()))
				}
				backButtons++
				assertClickActsAsKey(t, screen.build, (back.x0+back.x1-1)/2, 0, keyLabel(actBack))
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
					assertClickActsAsKey(t, dialog.build, (h.x0+h.x1-1)/2, (h.y0+h.y1-1)/2, key)
				}
				for label := range dialog.buttons {
					if !seen[label] {
						t.Errorf("the button %q has no target", label)
					}
				}
			})
		}
	}
	if backButtons == 0 {
		t.Fatal("no screen drew a back button")
	}
}

// assertClickActsAsKey builds two equal screens, presses key on one and clicks the cell on the other, and
// requires the same screen, focus, selection and view.
func assertClickActsAsKey(t *testing.T, build func(t *testing.T) *Model, x, y int, key string) {
	t.Helper()
	pressed, clicked := build(t), build(t)
	pressed.render()
	clicked.render()
	msg, ok := keyPress(key)
	if !ok {
		t.Fatalf("%q is no key a control may send", key)
	}
	_, cmd := pressed.Update(msg)
	deliver(pressed, cmd)
	clickAt(t, clicked, x, y)
	if want, got := clickState(pressed), clickState(clicked); want != got {
		t.Errorf("a click at (%d,%d) does not act as the key %q:\nkey:\n%s\nclick:\n%s", x, y, key, want, got)
	}
}

// hitAt reports whether a click target stands at the cell.
func (m *Model) hitAt(x, y int) bool {
	for _, group := range [][]hit{m.hits, m.frameHits, m.barHits} {
		for _, h := range group {
			if x >= h.x0 && x < h.x1 && y >= h.y0 && y < h.y1 {
				return true
			}
		}
	}
	return false
}
