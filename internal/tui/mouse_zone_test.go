package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// focusState is where the focus stands, as far as the keys and the clicks can move it.
func focusState(m *Model) string {
	logs := logFocus(0)
	if m.screen == screenLogs {
		logs = m.logs.focus
	}
	return fmt.Sprint(m.screen, " sub:", m.navSub, " bar:", focusedBarField(m), " detail:", m.detailFocus, " notice:", m.noticeFocus,
		" logs:", logs)
}

// zoneModel is a model of the given size standing on the bars of section s.
func zoneModel(t *testing.T, w, h int, s section) *Model {
	t.Helper()
	m := sizedModel(groupModel(t), w, h)
	deliver(m, m.openSection(s))
	m.focusNav()
	m.render()
	return m
}

// zonesOf are the focus zones of the frame drawn last, lowest first: the bridged ones and the free area of the
// filter bar, which the bar names itself.
func zonesOf(m *Model) []hit {
	zones := append([]hit{}, m.zones...)
	for _, t := range m.barTargets {
		if t.Item == "" && t.Action == "" {
			zones = append(zones, hit{x0: t.Rect.X, y0: t.Rect.Y, x1: t.Rect.X + t.Rect.Width, y1: t.Rect.Y + t.Rect.Height})
		}
	}
	return zones
}

// freeCell finds a cell of zone i of zonesOf that no target ranked above it covers.
func freeCell(m *Model, i int) (x, y int, ok bool) {
	zones := zonesOf(m)
	z := zones[i]
	above := append(append([]hit{}, zones[i+1:]...), m.hits...)
	elements := append(append([]kittui.Target{}, m.frameTabs...), m.frameKeys...)
	for _, t := range m.barTargets {
		if t.Item != "" || t.Action != "" {
			elements = append(elements, t)
		}
	}
	for _, t := range elements {
		above = append(above, hit{x0: t.Rect.X, y0: t.Rect.Y, x1: t.Rect.X + t.Rect.Width, y1: t.Rect.Y + t.Rect.Height})
	}
	for y := z.y0; y < z.y1; y++ {
		for x := z.x0; x < z.x1; x++ {
			covered := false
			for _, h := range above {
				if x >= h.x0 && x < h.x1 && y >= h.y0 && y < h.y1 {
					covered = true
					break
				}
			}
			if !covered {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// Everything the tab key reaches is reached by a click on the free area of its zone, and the click leaves the
// focus where tabbing there does.
func TestFreeAreaOfAZoneFocusesLikeTab(t *testing.T) {
	sections := []section{sectionOverview, sectionConnections, sectionServices, sectionCredentials, sectionLogs,
		sectionTokens, sectionApprovals, sectionDefaults}
	for _, size := range []struct{ w, h int }{{80, 24}, {140, 40}, {minimumWidth, minimumHeight}} {
		for _, s := range sections {
			t.Run(fmt.Sprintf("%v/%dx%d", s, size.w, size.h), func(t *testing.T) {
				m := zoneModel(t, size.w, size.h, s)
				stops := []string{focusState(m)}
				for range 10 {
					deliverKey(t, m, "tab")
					st := focusState(m)
					if slices.Contains(stops, st) {
						break
					}
					stops = append(stops, st)
				}
				allowed := map[string]bool{}
				for _, st := range stops {
					allowed[st] = true
				}
				// The tab line is a stop of its own that tab does not reach where the area has an option bar.
				bar := zoneModel(t, size.w, size.h, s)
				bar.navSub = false
				allowed[focusState(bar)] = true

				reached := map[string]bool{}
				for from := range stops {
					probe := zoneModel(t, size.w, size.h, s)
					for range from {
						deliverKey(t, probe, "tab")
					}
					probe.render()
					for i := range zonesOf(probe) {
						x, y, ok := freeCell(probe, i)
						if !ok {
							t.Errorf("stop %d: zone %d has no free cell", from, i)
							continue
						}
						m := zoneModel(t, size.w, size.h, s)
						for range from {
							deliverKey(t, m, "tab")
						}
						m.render()
						clickAt(t, m, x, y)
						st := focusState(m)
						if !allowed[st] {
							t.Errorf("stop %d: click on zone %d at %d,%d gives %q, want one of %q", from, i, x, y, st, slices.Sorted(maps.Keys(allowed)))
						}
						reached[st] = true
					}
				}
				for _, st := range stops {
					if !reached[st] && st != stops[0] {
						// The detail pane and the date field of the Logs are elements with targets of their own.
						if strings.Contains(st, "detail:true") || st == fmt.Sprint(screenLogs, " sub:false bar: detail:false notice:false logs:", logFocusDate) {
							continue
						}
						t.Errorf("no zone reaches the stop %q", st)
					}
				}
			})
		}
	}
}

func deliverKey(t *testing.T, m *Model, key string) {
	t.Helper()
	pump(t, m, key)
}

// A page of the settings is entered by a click into its workspace, as enter does, and the form has the focus.
func TestClickIntoTheWorkspaceOfASettingsPageEntersIt(t *testing.T) {
	for _, s := range []section{sectionGeneral, sectionVault, sectionTransfer, sectionUpdate, sectionSelftest} {
		t.Run(fmt.Sprint(s), func(t *testing.T) {
			m := sizedModel(groupModel(t), 120, 40)
			clickOn(t, m, "6 Settings")
			if m.screen != screenNav || m.section != sectionGeneral {
				t.Fatalf("after the click on the tab: screen %v section %v", m.screen, m.section)
			}
			if s != sectionGeneral {
				m.previewSection(s)
				m.render()
			}
			clickAt(t, m, 4, m.workspaceTop()+3)
			if m.screen != screenForm || m.section != s || m.fields == nil {
				t.Errorf("after the click into the workspace: screen %v section %v, want the form of %v", m.screen, m.section, s)
			}
		})
	}
}

// A click on the free area of a zone never types and never acts: the only keys it stands for move the focus.
func TestZoneClickNeverMakesText(t *testing.T) {
	m := zoneModel(t, 120, 40, sectionServices)
	for i := range zonesOf(m) {
		if x, y, ok := freeCell(m, i); ok {
			_, cmd := m.Update(mouseEvent(x, y, tea.MouseLeft))
			deliver(m, cmd)
		}
	}
	if m.list.Query() != "" || m.list.Searching() {
		t.Errorf("a click typed: query %q editing %v", m.list.Query(), m.list.Searching())
	}
}
