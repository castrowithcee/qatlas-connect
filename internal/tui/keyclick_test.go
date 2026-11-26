package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/helptopics"
	kithelp "github.com/castrowithcee/qatlas-kit/helptopics"
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
			m.screen, m.section, m.navZone = screenNav, sectionOverview, optionBar(false)
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
		{"defaults list filtered", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionDefaults)
			press(t, m, "/")
			typeText(t, m, "a")
			press(t, m, "enter")
			return m
		}},
		{"connections filter bar", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionConnections)
			press(t, m, firstKey(t, actFilter), "tab")
			return m
		}},
		{"services filter values", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionServices)
			press(t, m, firstKey(t, actFilter), "tab", "enter")
			return m
		}},
		{"credentials filter button", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionCredentials)
			m = sizedModel(m, 44, 30)
			m.list.SetFilter("reader")
			return m
		}},
		{"connections filter modal", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionConnections)
			m = sizedModel(m, 44, 30)
			press(t, m, firstKey(t, actFilter), "enter", "down")
			return m
		}},
		{"connections filter modal in the workspace", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 140, 40)
			openSectionByName(t, m, sectionConnections)
			m = sizedModel(m, minimumWidth, minimumHeight)
			press(t, m, firstKey(t, actFilter), "enter", "down")
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

// optionBar is the line of the navigation on the option bar, or with on false on the tab line.
func optionBar(on bool) kittui.FocusZone {
	if on {
		return kittui.ZoneSubtabs
	}
	return kittui.ZoneTabs
}

// onOptionBar reports whether the navigation stands on the option bar of the area.
func (m *Model) onOptionBar() bool { return m.navLine() == kittui.ZoneSubtabs }

// state is what a key and a click must leave alike: the screen, the focus, the selection and the view. The
// view names the configuration file, which every model of a test keeps in a directory of its own.
var shownConfigPath = regexp.MustCompile(`\S*config\.yaml`)

func clickState(m *Model) string {
	selected, _ := m.marked()
	view := strings.ReplaceAll(m.render(), filepath.Dir(m.svc.Path()), "<dir>")
	// Each model has its own temporary directory, and a shortened path shows only its end.
	view = shownConfigPath.ReplaceAllString(view, "<config>")
	return fmt.Sprintf("screen %v section %v focus %d line %v path area %v selected %q\n%s", m.screen, m.section, m.focus, m.navLine(), m.pathArea, selected, view)
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
// the footer, and the tabs, whose click on the tab line is the digit of the area, and the sections of the path
// and the choice field, whose click is enter on them. The dialog answers the key line names are targets as
// well. The keys come from the keymap, also after the preferences rebound some, in every navigation form.
func TestEveryScreenControlHasATargetAndClicksAsItsKey(t *testing.T) {
	backButtons, footerTargets := 0, 0
	var listTargets listTargetCount
	var barTargets barTargetCount
	for _, form := range PrefsSchema().Navigations {
		for _, binding := range []struct {
			name string
			keys map[action]string
		}{{"default keys", nil}, {"rebound keys", map[action]string{actInfo: "f3", actSave: "f4", actHelp: "ctrl+x", actCommand: "f6", actLock: "ctrl+l"}}} {
			for _, screen := range frameScreens() {
				build := func(t *testing.T) *Model {
					m := screen.build(t)
					m.prefs.Navigation = form
					return m
				}
				t.Run(form+"/"+binding.name+"/"+screen.name, func(t *testing.T) {
					keepKeys(t)
					applyKeys(binding.keys)
					base := build(t)
					base.render()
					barTargets.add(assertBarTargetsClickAsKeys(t, base, build))
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
						assertClickActsAsKey(t, build, x, y, firstKey(t, target.Action))
					}
					listTargets.add(assertListTargetsClickAsKeys(t, base, build))
					if base.screen != screenNav || base.onOptionBar() {
						return
					}
					// On the focused tab line the arrows reach a tab as its click does: the area opens there and the
					// line keeps the focus; the sub-tabs lie one line down. On the path left and right choose its
					// section and enter opens it, as on the choice field.
					active := base.section.area()
					for _, target := range base.frameTabs {
						a, _ := areaByName(string(target.Area))
						keys := slices.Repeat([]string{"right"}, (int(a)-int(active)+int(areaCount))%int(areaCount))
						switch {
						case target.Zone == kittui.ZonePath && target.Tab == "":
							keys = []string{"left", "enter"}
						case target.Zone == kittui.ZonePath:
							keys = []string{"right", "enter"}
						case target.Zone == kittui.ZoneTabs && base.navForm() == kittui.NavHeaderChoice:
							keys = []string{"enter"}
						case target.Tab != "":
							at := slices.IndexFunc(areaSections[a], func(s section) bool { return s.key() == string(target.Tab) })
							keys = append([]string{"down"}, slices.Repeat([]string{"right"}, at-base.section.index())...)
						}
						assertClickActsAsKey(t, build, target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y, keys...)
					}
				})
			}
			for _, help := range clickHelps() {
				build := func(t *testing.T) *Model {
					m := help.build(t)
					m.prefs.Navigation = form
					// The targets of the open help come from the last drawing, which the form changes.
					m.render()
					return m
				}
				t.Run(form+"/"+binding.name+"/"+help.name, func(t *testing.T) {
					keepKeys(t)
					applyKeys(binding.keys)
					assertHelpTargetsClickAsKeys(t, build, help.lists)
				})
			}
			if form == PrefsSchema().DefaultNavigation {
				t.Run(form+"/"+binding.name+"/explanation", func(t *testing.T) {
					keepKeys(t)
					applyKeys(binding.keys)
					assertExplanationTargetsClickAsKeys(t)
				})
			}
			for _, dialog := range clickDialogs() {
				build := func(t *testing.T) *Model {
					m := dialog.build(t)
					m.prefs.Navigation = form
					return m
				}
				t.Run(form+"/"+binding.name+"/"+dialog.name, func(t *testing.T) {
					keepKeys(t)
					applyKeys(binding.keys)
					base := build(t)
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
						assertClickActsAsKey(t, build, (h.x0+h.x1-1)/2, (h.y0+h.y1-1)/2, kittui.KeyName(msg))
					}
					for label := range dialog.buttons {
						if !seen[label] {
							t.Errorf("the button %q has no target", label)
						}
					}
				})
			}
		}
	}
	if backButtons == 0 || footerTargets == 0 {
		t.Fatalf("back buttons %d, footer targets %d: the screens drew no such control", backButtons, footerTargets)
	}
	if listTargets.rows == 0 || listTargets.groups == 0 || listTargets.search == 0 || listTargets.free == 0 {
		t.Fatalf("list targets %+v: the lists drew no such target", listTargets)
	}
	if barTargets.fields == 0 || barTargets.values == 0 || barTargets.button == 0 || barTargets.modalFields == 0 ||
		barTargets.modalValues == 0 || barTargets.search == 0 || barTargets.free == 0 || barTargets.keys == 0 {
		t.Fatalf("filter bar targets %+v: the bars drew no such target", barTargets)
	}
}

// barTargetCount counts the targets of the filter bars by kind.
type barTargetCount struct{ fields, values, button, modalFields, modalValues, search, free, keys int }

func (c *barTargetCount) add(n barTargetCount) {
	c.fields, c.values, c.button = c.fields+n.fields, c.values+n.values, c.button+n.button
	c.modalFields, c.modalValues = c.modalFields+n.modalFields, c.modalValues+n.modalValues
	c.search, c.free, c.keys = c.search+n.search, c.free+n.free, c.keys+n.keys
}

// barFieldIndex is the place of the field id among the fields of the bar of the active section.
func barFieldIndex(m *Model, id string) int {
	return slices.IndexFunc(barKinds(m.section), func(k barKind) bool { return string(barIDs[k]) == id })
}

// barMoves are the keys that move from place from to place to, down or up.
func barMoves(from, to int, down, up string) []string {
	if to < from {
		return slices.Repeat([]string{up}, from-to)
	}
	return slices.Repeat([]string{down}, to-from)
}

// assertBarTargetsClickAsKeys clicks the middle of every target the filter bar, its open values and its modal
// drew, and requires the keys that reach it and press it: a field is the filter key and tab up to it and
// enter, or esc where its values are open; a value the moves from the value in force and enter; the button
// the filter key and enter; a field of the modal the moves to it, its value those and right; the search field
// the search key, after esc where values are open; a key of the modal its first key; the free area of the bar
// the filter key unless the bar has the focus.
func assertBarTargetsClickAsKeys(t *testing.T, base *Model, build func(t *testing.T) *Model) barTargetCount {
	t.Helper()
	var n barTargetCount
	focus := -1
	if label := focusedBarField(base); label != "" {
		focus = slices.IndexFunc(barKinds(base.section), func(k barKind) bool { l, _, _ := base.barField(k); return l == label })
	}
	open := base.barValuesOpen()
	modal := base.barOpen() && !open
	reach := func() []string {
		if focus < 0 && !base.barFocused() {
			return []string{firstKey(t, actFilter)}
		}
		return nil
	}
	elements := func(x, y int) bool {
		for _, o := range base.barTargets {
			r := o.Rect
			if (o.Item != "" || o.Action != "") && x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
				return true
			}
		}
		return false
	}
	enter := firstKey(t, actEnter)
	for _, target := range base.barTargets {
		r := target.Rect
		x, y := r.X+(r.Width-1)/2, r.Y
		var keys []string
		switch item := target.Item; {
		case strings.HasPrefix(item, "\x00mf"), strings.HasPrefix(item, "\x00mv"):
			// The modal opens on its first field, and the screens move it one down.
			to := barFieldIndex(base, item[3:])
			keys = barMoves(1, to, "down", "up")
			if strings.HasPrefix(item, "\x00mv") {
				n.modalValues++
				keys = append(keys, "right")
			} else {
				n.modalFields++
			}
		case strings.HasPrefix(item, "\x00f"):
			n.fields++
			to := barFieldIndex(base, item[2:])
			switch {
			case open && to == focus:
				keys = []string{firstKey(t, actBack)}
			case open:
				keys = append([]string{firstKey(t, actBack)}, barMoves(focus, to, "tab", "shift+tab")...)
				keys = append(keys, enter)
			default:
				keys = append(reach(), barMoves(max(focus, 0), to, "tab", "shift+tab")...)
				keys = append(keys, enter)
			}
		case strings.HasPrefix(item, "\x00o"):
			n.values++
			field, value, _ := strings.Cut(item[2:], "\x00")
			k := barKinds(base.section)[barFieldIndex(base, field)]
			_, values, active := base.barField(k)
			keys = append(barMoves(active, slices.Index(values, value), "down", "up"), enter)
		case item == "\x00b":
			n.button++
			keys = append(reach(), enter)
		case item != "":
			t.Errorf("the bar target %q is of no known kind", item)
			continue
		case target.Action == actSearch:
			n.search++
			if open {
				keys = []string{firstKey(t, actBack)}
			}
			keys = append(keys, firstKey(t, actSearch))
		case target.Action != "":
			if !modal {
				t.Errorf("the key target %q stands outside the modal", target.Action)
			}
			n.keys++
			keys = []string{firstKey(t, target.Action)}
		default:
			for x = r.X; x < r.X+r.Width && elements(x, y); x++ {
			}
			if x == r.X+r.Width {
				continue
			}
			n.free++
			keys = reach()
		}
		assertClickActsAsKey(t, build, x, y, keys...)
	}
	return n
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

// clickHelps are the help over the screen, at the top of its first topic and at its end with the key lists,
// and the help in the workspace, whose keys stand in the footer. lists tests only the lines of the key lists,
// which the other cases leave out.
func clickHelps() []struct {
	name  string
	build func(t *testing.T) *Model
	lists bool
} {
	open := func(w, h int, end bool) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), w, h)
			openSectionByName(t, m, sectionConnections)
			m.openHelp()
			m.render()
			if end {
				press(t, m, firstKey(t, actEnd))
				m.render()
			}
			return m
		}
	}
	return []struct {
		name  string
		build func(t *testing.T) *Model
		lists bool
	}{{"help over the screen", open(140, 40, false), false}, {"help with its key lists", open(140, 60, true), true},
		{"help in the workspace", open(43, 30, true), false}}
}

// assertHelpTargetsClickAsKeys requires each target of the help to act as its key: a topic tab as the
// right key up to that topic, a key of the key line or the footer as that key, and a line of the key lists as
// the back key that closes the help and then the key of the line; a notch of the wheel over the text is three
// presses of down.
func assertHelpTargetsClickAsKeys(t *testing.T, build func(t *testing.T) *Model, lists bool) {
	t.Helper()
	base := build(t)
	h := helpOf(base)
	if h == nil {
		t.Fatal("the help is not open")
	}
	topics := helptopics.All()
	nav := []action{actLeft, actRight, actTopicNext, actTopicPrev, actUp, actDown, actPageUp, actPageDown, actHome, actEnd, actBack, actHelp}
	tabs, keys, listed := 0, 0, 0
	for _, target := range base.modalTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		switch name, tab := strings.CutPrefix(target.Item, "\x00t"); {
		case tab:
			tabs++
			if lists {
				break
			}
			at := slices.IndexFunc(topics, func(topic kithelp.Topic) bool { return topic.Name == name })
			// The tab of the topic shown starts it at its top again.
			keys := []string{firstKey(t, actHome)}
			if from := helpTopicIndex(t, base); at != from {
				keys = slices.Repeat([]string{firstKey(t, actRight)}, (at-from+len(topics))%len(topics))
			}
			assertClickActsAsKey(t, build, x, y, keys...)
		case target.Action == "":
		case slices.Contains(nav, target.Action):
			keys++
			if !lists {
				assertClickActsAsKey(t, build, x, y, firstKey(t, target.Action))
			}
		default:
			listed++
			assertClickActsAsKey(t, build, x, y, firstKey(t, actBack), firstKey(t, target.Action))
		}
	}
	if tabs != len(topics) || keys == 0 {
		t.Errorf("%d topic tabs and %d keys are targets", tabs, keys)
	}
	if lists && listed == 0 {
		t.Error("no line of the key lists is a target")
	}
	pressed, wheeled := build(t), build(t)
	press(t, pressed, firstKey(t, actDown), firstKey(t, actDown), firstKey(t, actDown))
	wheelEvent(t, wheeled, base.termWidth/2, base.termHeight/2, tea.MouseWheelDown)
	if want, got := clickState(pressed), clickState(wheeled); want != got {
		t.Errorf("a notch of the wheel does not act as three presses of down:\nkey:\n%s\nwheel:\n%s", want, got)
	}
}

// clickExplanationURL is the address of the explanation of the click test.
const clickExplanationURL = "https://example.org/explained"

// assertExplanationTargetsClickAsKeys requires each key of the explanation over the screen to act as its key,
// a click on its address, elsewhere in its frame and outside it to act as the back key that closes it, and a
// notch of the wheel over it to act as three presses of down. The text is long enough to scroll; the key line
// leaves out the keys that do not fit it.
func assertExplanationTargetsClickAsKeys(t *testing.T) {
	t.Helper()
	open := func(end bool) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 100, 30)
			openSectionByName(t, m, sectionServices)
			m.openExplanation(infoSubject{title: "Long", long: longText(300), doc: clickExplanationURL}.withTerm("term.service"))
			m.render()
			if end {
				press(t, m, firstKey(t, actEnd))
				m.render()
			}
			return m
		}
	}
	build := open(false)
	base := build(t)
	if base.explanation() == nil {
		t.Fatal("the explanation is not open")
	}
	var keys []action
	frame, outside := false, false
	for _, target := range base.modalTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		switch {
		case target.Action != "":
			keys = append(keys, target.Action)
			assertClickActsAsKey(t, build, x, y, firstKey(t, target.Action))
		case target.Zone == kittui.ZoneOutside:
			outside = true
			assertClickActsAsKey(t, build, x, y+target.Rect.Height/2, firstKey(t, actBack))
		case target.Item != "":
			frame = true
			assertClickActsAsKey(t, build, x, y, firstKey(t, actBack))
		}
	}
	for _, a := range []action{actUp, actDown, actPageUp, actPageDown, actCopy, actBack, actInfo} {
		if !slices.Contains(keys, a) {
			t.Errorf("the key %q of the explanation is no target; targets: %q", a, keys)
		}
	}
	if !frame || !outside {
		t.Errorf("the frame is a target %v, the screen outside it %v", frame, outside)
	}
	x, y := cellOf(t, open(true)(t), clickExplanationURL)
	assertClickActsAsKey(t, open(true), x+len(clickExplanationURL)/2, y, firstKey(t, actBack))
	pressed, wheeled := build(t), build(t)
	press(t, pressed, firstKey(t, actDown), firstKey(t, actDown), firstKey(t, actDown))
	wheelEvent(t, wheeled, base.termWidth/2, base.termHeight/2, tea.MouseWheelDown)
	if want, got := clickState(pressed), clickState(wheeled); want != got {
		t.Errorf("a notch of the wheel does not act as three presses of down:\nkey:\n%s\nwheel:\n%s", want, got)
	}
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
func (m *Model) modalShown() bool {
	d := m.dialogBox
	return m.modal != nil || d.x1 > d.x0 && d.y1 > d.y0
}

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
	if m.navForm() != kittui.NavTabs {
		// The path and the choice field name the current place alone, a short path only its last section, and
		// the guided setup none.
		fields := 0
		for _, target := range m.frameTabs {
			if target.Zone == kittui.ZonePath || target.Zone == kittui.ZoneTabs {
				fields++
			}
		}
		if (fields > 0) == (m.wizard != nil) || fields > 2 {
			t.Errorf("%d targets of the path or the choice field:\n%s", fields, strings.Join(lines[:3], "\n"))
		}
		return
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
