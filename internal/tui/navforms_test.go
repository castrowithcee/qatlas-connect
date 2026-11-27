package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// overlayForms are the navigation forms with an overlay.
var overlayForms = []string{kitprefs.NavigationPathLine, kitprefs.NavigationPathHeader, kitprefs.NavigationHeaderChoice}

// navModel is an editor of the given size in the form, with the tab line or the path on the Credentials.
func navModel(t *testing.T, form string, w, h int) *Model {
	t.Helper()
	m := sizedModel(groupModel(t), w, h)
	openSectionByName(t, m, sectionCredentials)
	pump(t, m, "shift+tab")
	m.prefs.Navigation = form
	if m.screen != screenNav {
		t.Fatalf("screen %v, want the navigation", m.screen)
	}
	return m
}

func TestNavigationAreasPassTheKitChecks(t *testing.T) {
	m := groupModel(t)
	p, err := kittui.NewPathNav(pathNavKeys, keymap, translator(false))
	mustNoError(t, err)
	for _, areas := range [][]kittui.Area{m.kitAreas(), m.navSpec().Areas} {
		mustNoError(t, kittui.CheckAreas(areas))
		mustNoError(t, p.CheckAreas(areas))
	}
	for _, form := range PrefsSchema().Navigations {
		m.prefs.Navigation = form
		mustNoError(t, kittui.CheckFrame(m.frameSpec()))
	}
}

// The menu key opens the overview only in the forms with an overlay, and only where nothing is typed; its hint
// stands in the footer there alone.
func TestMenuKeyOpensTheOverviewOnlyInTheFormsWithOne(t *testing.T) {
	for _, form := range PrefsSchema().Navigations {
		m := navModel(t, form, 150, 42)
		hinted := strings.Contains(frameLines(m)[41], "m menu")
		pump(t, m, "m")
		if want := form != kitprefs.NavigationTabs; (m.menu != nil) != want || hinted != want {
			t.Errorf("%s: m opens %v, hinted %v; want %v", form, m.menu != nil, hinted, want)
		}
		m = navModel(t, form, 150, 42)
		pump(t, m, "tab", "/", "m")
		if m.menu != nil || m.list.Query() != "m" {
			t.Errorf("%s: m while searching opens %v, query %q; want it typed", form, m.menu != nil, m.list.Query())
		}
	}
}

// An open overlay takes every key and click; the screen behind it takes none. Its choice is the click on the
// tab, and back closes it.
func TestOpenOverlayLocksTheScreenBehindIt(t *testing.T) {
	for _, form := range overlayForms {
		m := navModel(t, form, 150, 42)
		pump(t, m, "m")
		help := strings.Index(frameLines(m)[0], "[?] help")
		pump(t, m, "q", "3", keyLabel(actHelp))
		clickAt(t, m, help+1, 0)
		if m.menu == nil || m.quitting || m.section != sectionCredentials || m.screen != screenNav {
			t.Fatalf("%s: behind the overlay: menu %v quit %v section %v screen %v", form, m.menu != nil, m.quitting, m.section, m.screen)
		}
		pump(t, m, "esc")
		if m.menu != nil || m.section != sectionCredentials {
			t.Fatalf("%s: esc leaves menu %v section %v", form, m.menu != nil, m.section)
		}
		// The overview lists Overview and Library first, so Connections is three rows down from Credentials.
		pump(t, m, "m", "down", "enter")
		if m.menu != nil || m.section != sectionConnections || m.screen != screenNav || m.navFocus() != m.navZones()[0] {
			t.Errorf("%s: choice = menu %v section %v screen %v focus %v", form, m.menu != nil, m.section, m.screen, m.navFocus())
		}
	}
}

// A click on the path or the choice field leaves a form as a click on a tab does: unsaved input asks first.
func TestPathClickAsksBeforeUnsavedInputIsLost(t *testing.T) {
	for _, form := range overlayForms {
		m := sizedModel(groupModel(t), 150, 42)
		openSectionByName(t, m, sectionConnections)
		m.prefs.Navigation = form
		pressNew(t, m)
		typeText(t, m, "draft")
		m.render()
		if len(m.frameTabs) == 0 {
			t.Fatalf("%s: the form draws no path target", form)
		}
		r := m.frameTabs[0].Rect
		clickAt(t, m, r.X+r.Width/2, r.Y)
		if m.screen != screenLeave || m.menu != nil {
			t.Errorf("%s: click on the path from a changed form = screen %v menu %v, want the question", form, m.screen, m.menu != nil)
		}
	}
}

// The guided setup stands in the path as text without a target, and the menu key opens nothing there.
func TestGuidedSetupIsTheModeOfThePath(t *testing.T) {
	for _, form := range overlayForms {
		m := sizedModel(groupModel(t), 150, 42)
		openSectionByName(t, m, sectionConnections)
		m.prefs.Navigation = form
		m.startSetup()
		pump(t, m, "enter")
		lines := frameLines(m)
		if len(m.frameTabs) != 0 || !strings.Contains(strings.Join(lines[:2], "\n"), tr(tTabSetup)) {
			t.Errorf("%s: setup targets %+v:\n%s", form, m.frameTabs, strings.Join(lines[:3], "\n"))
		}
		if m.menuReachable() || m.openMenu((*kittui.PathNav).OpenOverview) {
			t.Errorf("%s: the overview opens during the setup", form)
		}
	}
}

// Every target of an open overlay clicks as its keys: a row as the moves to it and enter, a key of its key
// line as that key; the wheel moves as the arrows, up from the marked row, since only the keys wrap. Each opener is tried, also where the overlay fills the
// workspace of a narrow terminal.
func TestOverlayTargetsClickAsTheirKeys(t *testing.T) {
	for _, form := range overlayForms {
		for _, size := range [][2]int{{150, 42}, {40, 15}} {
			openers := map[string][]string{"overview": {"m"}}
			if form == kitprefs.NavigationHeaderChoice {
				openers["areas"] = []string{"up", "enter"}
			} else {
				openers["choice"] = []string{"enter"}
			}
			for name, keys := range openers {
				build := func(t *testing.T) *Model {
					m := navModel(t, form, size[0], size[1])
					pump(t, m, keys...)
					if m.menu == nil {
						t.Fatalf("%s %v %s: no overlay", form, size, name)
					}
					return m
				}
				base := build(t)
				base.render()
				lines := frameLines(base)
				marked := -1
				for _, tg := range base.menuTargets {
					if tg.Item != "" && strings.HasPrefix(ansi.Cut(lines[tg.Rect.Y], tg.Rect.X, tg.Rect.X+tg.Rect.Width), ">") {
						marked = tg.Rect.Y
					}
				}
				if marked < 0 {
					t.Fatalf("%s %v %s: no marked row:\n%s", form, size, name, strings.Join(lines, "\n"))
				}
				rows, keyTargets := 0, 0
				for _, tg := range base.menuTargets {
					var keys []string
					switch {
					case tg.Item != "":
						move := "down"
						if tg.Rect.Y < marked {
							move = "up"
						}
						keys = append(slices.Repeat([]string{move}, max(tg.Rect.Y-marked, marked-tg.Rect.Y)), "enter")
						rows++
					case tg.Action != "":
						keys = []string{firstKey(t, tg.Action)}
						keyTargets++
					default:
						continue
					}
					assertClickActsAsKey(t, build, tg.Rect.X+(tg.Rect.Width-1)/2, tg.Rect.Y, keys...)
				}
				if rows < 2 || keyTargets == 0 {
					t.Errorf("%s %v %s: %d rows and %d keys are targets", form, size, name, rows, keyTargets)
				}
				wheeled, pressed := build(t), build(t)
				wheelAt(t, wheeled, size[0]/2, size[1]/2, tea.MouseWheelUp)
				pump(t, pressed, "up")
				if want, got := clickState(pressed), clickState(wheeled); want != got {
					t.Errorf("%s %v %s: the wheel does not move as up:\nkey:\n%s\nwheel:\n%s", form, size, name, want, got)
				}
			}
		}
	}
}

// The navigation row of General applies the form once saved.
func TestGeneralNavigationRowAppliesOnSave(t *testing.T) {
	m, path := lockedVaultModel(t)
	openSectionByName(t, m, sectionGeneral)
	focusField(t, m, navLabel)
	press(t, m, "right")
	pump(t, m, "f2")
	got, _ := kitprefs.Load(path, PrefsSchema())
	if m.prefs.Navigation != kitprefs.NavigationPathLine || got.Navigation != kitprefs.NavigationPathLine {
		t.Fatalf("navigation = %q, stored %q; want pathline", m.prefs.Navigation, got.Navigation)
	}
	if line := frameLines(m)[1]; !strings.Contains(line, "Settings ▾ › General ▾") {
		t.Errorf("the frame after the save does not show the path:\n%s", line)
	}
}
