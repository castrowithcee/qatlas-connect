package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/helptopics"
	"github.com/castrowithcee/qatlas-connect/internal/provider/github"
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
		{"export selection", func(t *testing.T) *Model {
			return exportAfter(t, "down", " ", "down")
		}},
		{"export selection filtered", func(t *testing.T) *Model {
			return exportAfter(t, "/", "w", "i", "enter", "down", " ")
		}},
		{"export mode", func(t *testing.T) *Model {
			return exportAfter(t, "up", " ", "enter")
		}},
		{"target list", func(t *testing.T) *Model {
			m, _, _ := plainTargetsModel(t)
			openConnection(t, m, "gh")
			openTargetList(t, m)
			return m
		}},
		{"target add menu", func(t *testing.T) *Model {
			m, _, _ := knownTargetsModel(t)
			openConnection(t, m, "gh-a")
			openTargetList(t, m)
			press(t, m, "n")
			return m
		}},
		{"target picker", func(t *testing.T) *Model {
			// A kept filter that every entry matches shows the search line beside the rows and their boxes.
			m := addMenuOn(t, listerRegistry(t), &listSource{list: pickPage(group("a"), group("b"), group("c"))})
			deliver(m, m.startPick("group"))
			pump(t, m, "space", "/", "g", "enter")
			return m
		}},
		{"findings", func(t *testing.T) *Model {
			s := newFindScene(t)
			s.renameByHand(t, "credential", "reader", "reader2")
			m := s.open(t)
			sizedModel(m, 140, 40)
			openSectionByName(t, m, sectionCredentials)
			m.openFindings()
			return m
		}},
	}
}

// exportAfter opens the export by the command line, with the keys in force, and presses keys on its selection.
func exportAfter(t *testing.T, keys ...string) *Model {
	t.Helper()
	v, _ := plainExportModel(t)
	// The file question shows the start directory, which would differ in every model.
	v.m.startDir = filepath.Join(string(filepath.Separator), "start")
	pump(t, v.m, firstKey(t, actCommand))
	typeText(t, v.m, "export")
	pump(t, v.m, "enter")
	if v.m.screen != screenTransfer || v.m.export == nil {
		t.Fatalf("the command export did not open the selection: screen %v", v.m.screen)
	}
	pump(t, v.m, keys...)
	return v.m
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

var shownID = regexp.MustCompile(`[0-9a-f]{5,}`)

// shownClock is the time in the header of the Overview, which turns to the next minute between the key and
// the click when they straddle it.
var shownClock = regexp.MustCompile(`\b[0-2][0-9]:[0-5][0-9]\b`)

// fixIDs gives every entry an id of its own derived from its kind and name, the same in every model of a test,
// where a view shows the ids in full and a narrow one wraps them at any character.
func fixIDs(t *testing.T, m *Model) {
	t.Helper()
	id := func(kind, name string) string {
		sum := sha256.Sum256([]byte(kind + "/" + name))
		return hex.EncodeToString(sum[:16])
	}
	for name, s := range m.cfg.Services {
		s.ID = id("service", name)
		m.cfg.Services[name] = s
	}
	for name, c := range m.cfg.Credentials {
		c.ID = id("credential", name)
		m.cfg.Credentials[name] = c
	}
	for name, c := range m.cfg.Connections {
		c.ID = id("connection", name)
		m.cfg.Connections[name] = c
	}
	mustNoError(t, m.store.Save(m.cfg))
	syncRevision(t, m)
}

func clickState(m *Model) string {
	selected, _ := m.marked()
	view := strings.ReplaceAll(m.render(), filepath.Dir(m.svc.Path()), "<dir>")
	// Each model has its own temporary directory, and a shortened path shows only its end; a seeded entry
	// has an ID of its own in every model, which a narrow row wraps.
	view = shownConfigPath.ReplaceAllString(view, "<config>")
	if m.section == sectionOverview {
		view = shownClock.ReplaceAllString(view, "<time>")
	}
	view = shownID.ReplaceAllStringFunc(view, func(s string) string {
		if strings.ContainsAny(s, "0123456789") {
			return "<id>"
		}
		return s
	})
	return fmt.Sprintf("screen %v section %v focus %d line %v path area %v selected %q\n%s", m.screen, m.section, m.focus, m.navLine(), m.pathArea, selected, view)
}

// clickDialogs are the questions of the kit's dialog, each over the screen and in the workspace of a terminal
// too narrow for that, whose footer then holds the answers.
func clickDialogs() []struct {
	name  string
	build func(t *testing.T) *Model
} {
	type dialogCase = struct {
		name  string
		build func(t *testing.T) *Model
	}
	over := func(set func(m *Model)) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			m := groupModel(t)
			openSectionByName(t, m, sectionCredentials)
			m.screen = screenConfirm
			set(m)
			return m
		}
	}
	overForm := func(set func(m *Model)) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			m, _ := startServiceForm(t)
			m.screen = screenConfirm
			set(m)
			return m
		}
	}
	var cases []dialogCase
	for _, q := range append(questionCases(), []dialogCase{
		{"delete question", func(t *testing.T) *Model {
			m := groupModel(t)
			openSectionByName(t, m, sectionCredentials)
			press(t, m, keyLabel(actDelete))
			if m.screen != screenConfirm {
				t.Fatalf("screen %v, want the delete question", m.screen)
			}
			return m
		}},
		{"remove secret question", overForm(func(m *Model) { m.confirmRole, m.editing = "key", "reader" })},
		{"lock question", over(func(m *Model) { m.vaultLockConfirm, m.vaultLockBack = true, screenList })},
		{"encryption question", overForm(func(m *Model) { m.decryptConfirm = true })},
		{"migrate question", overForm(func(m *Model) { m.migratePlanConfirm, m.migrate = true, &migratePlan{} })},
		{"migrate delete question", overForm(func(m *Model) {
			m.migrateDeleteConfirm, m.migrateResult = true, "2 entries were migrated"
		})},
		{"approve all question", over(func(m *Model) { m.approveAllConfirm = true })},
		{"revoke question", over(func(m *Model) { m.tokenRevoke = "agent" })},
		{"profile question", overForm(func(m *Model) { m.pendingProfile = "custom-profile" })},
	}...) {
		for _, size := range []struct {
			name          string
			width, height int
		}{{"", 140, 40}, {" in the workspace", minimumWidth + 3, 24}} {
			build := q.build
			cases = append(cases, dialogCase{q.name + size.name, func(t *testing.T) *Model {
				return sizedModel(build(t), size.width, size.height)
			}})
		}
	}
	return cases
}

// questionCases are the questions that stand over a screen other than screenConfirm.
func questionCases() []struct {
	name  string
	build func(t *testing.T) *Model
} {
	return []struct {
		name  string
		build func(t *testing.T) *Model
	}{
		{"leave question", func(t *testing.T) *Model {
			m, _ := startServiceForm(t)
			press(t, m, "esc")
			if m.screen != screenLeave {
				t.Fatalf("screen %v, want the leave question", m.screen)
			}
			return m
		}},
		{"quit question", func(t *testing.T) *Model {
			m, _ := startServiceForm(t)
			press(t, m, "esc", "ctrl+c")
			if m.screen != screenLeave || !m.leaveQuit {
				t.Fatalf("screen %v, want the leave question to quit", m.screen)
			}
			return m
		}},
		{"update question", func(t *testing.T) *Model {
			m := startedWith(t, newerRelease())
			press(t, m, "U")
			if m.screen != screenUpdate {
				t.Fatalf("screen %v, want the update question", m.screen)
			}
			return m
		}},
		{"version question", func(t *testing.T) *Model {
			m := startedVersions(t, choosable())
			openVersions(t, m)
			typeText(t, m, "rc")
			pump(t, m, "enter")
			if m.screen != screenUpdate || m.versionPending == nil {
				t.Fatalf("screen %v, want the version question", m.screen)
			}
			return m
		}},
		{"restore question", func(t *testing.T) *Model {
			fake := choosable()
			rollbackWithBackup(fake)
			m := startedVersions(t, fake)
			openVersions(t, m)
			typeText(t, m, "v0.3.1")
			pump(t, m, "enter", "y")
			if m.screen != screenUpdate || m.versionPending == nil || m.versionPending.plan == nil {
				t.Fatalf("screen %v, want the restore question", m.screen)
			}
			return m
		}},
		{"restart question", func(t *testing.T) *Model {
			m, _ := startServiceForm(t)
			m.updated, m.restartBack, m.screen = "v0.5.0", screenForm, screenRestart
			return m
		}},
		{"path remove question", func(t *testing.T) *Model {
			m, _ := toolsModel(t, targetsRegistry(t, github.Register), nil)
			openSectionByName(t, m, sectionConnections)
			pressNew(t, m)
			typeText(t, m, "gh")
			focusField(t, m, pathsLabel)
			press(t, m, "enter")
			// One path for every model, so a key and a click start from the same list; under the root, whose
			// few entries keep the suggestions quick.
			root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
			addPath(t, m, filepath.Join(root, "qconnect-click-path"))
			press(t, m, keyLabel(actDelete))
			if m.screen != screenPaths || !m.pathRemove {
				t.Fatalf("screen %v, want the remove question of the path list", m.screen)
			}
			return m
		}},
		{"target remove question", func(t *testing.T) *Model {
			m, _, _ := plainTargetsModel(t)
			openConnection(t, m, "gh")
			openTargetList(t, m)
			press(t, m, keyLabel(actDelete))
			if m.screen != screenTargets || !m.targetRemove {
				t.Fatalf("screen %v, want the remove question of the target list", m.screen)
			}
			return m
		}},
		{"rename question", func(t *testing.T) *Model {
			m, _, _ := newModel(t)
			seedConnections(t, m, "docs")
			fixIDs(t, m)
			startRename(t, m, sectionServices, "wiki", "handbook")
			pump(t, m, firstKey(t, actSave))
			if m.screen != screenRename {
				t.Fatalf("screen %v, want the rename question", m.screen)
			}
			return m
		}},
	}
}

// openVersions opens the version list by the command line, with the keys in force.
func openVersions(t *testing.T, m *Model) {
	t.Helper()
	pump(t, m, firstKey(t, actCommand))
	typeText(t, m, "version")
	pump(t, m, "enter")
	if m.screen != screenVersions {
		t.Fatalf("the command version did not open the list: screen %v", m.screen)
	}
}

// assertDialogTargetsClickAsKeys requires each target of the dialog of a question to act as its key: the
// buttons, the hints behind them and, in the workspace, the key groups of the footer. Over the screen a click
// beside the frame acts as esc, or does nothing where the question destroys something. A click on an input
// line only keeps it.
func assertDialogTargetsClickAsKeys(t *testing.T, build func(t *testing.T) *Model) {
	t.Helper()
	base := build(t)
	base.render()
	asked, _ := base.currentAsk()
	if base.question == nil || base.modal != base.question {
		t.Fatal("the question is no dialog")
	}
	var keys []action
	outside := false
	rows := 0
	for _, target := range base.modalTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		switch {
		case target.Action != "":
			keys = append(keys, target.Action)
			assertClickActsAsKey(t, build, x, y, firstKey(t, target.Action))
		case target.Zone == kittui.ZoneOutside:
			outside = true
			assertClickActsAsKey(t, build, x, y+target.Rect.Height/2, firstKey(t, actBack))
		case target.Zone == kittui.ZoneForm:
			// The input line comes first; a click on it keeps it. The first suggestion under it marks as down.
			switch rows++; rows {
			case 1:
				assertClickActsAsKey(t, build, x, y)
			case 2:
				assertClickActsAsKey(t, build, x, y, firstKey(t, actDown))
			}
		}
	}
	// The hints of the command line and the help give way before the answers on a short line.
	for _, a := range append([]action{actEnter}, answerActions(asked)...) {
		if !slices.Contains(keys, a) {
			t.Errorf("the key %q of the question is no target; targets: %q", a, keys)
		}
	}
	if popup := base.termWidth-8 >= 36; popup {
		danger := asked.danger != ""
		if outside == danger {
			t.Errorf("a click beside the question answers it %v, want %v", outside, !danger)
		}
		if danger {
			// A click on the tab line behind a destructive question answers nothing.
			assertClickActsAsKey(t, build, 2, 1)
		}
	}
}

// answerActions are the actions of the answers of q.
func answerActions(q ask) []action {
	var out []action
	for _, a := range q.answers {
		out = append(out, a.Action)
	}
	return out
}

// Every control of the frame is a target, and a click in its middle acts as the first key bound to its action,
// with every question and check of the key: the back button and the keys of the header, every key group of
// the footer, and the tabs, whose click on the tab line is the digit of the area, and the sections of the path
// and the choice field, whose click is enter on them. The dialog answers the key line names are targets as
// well. The keys come from the keymap, also after the preferences rebound some, in every navigation form.
func TestEveryScreenControlHasATargetAndClicksAsItsKey(t *testing.T) {
	var backButtons, footerTargets atomic.Int32
	var listTargets listTargetCount
	var barTargets barTargetCount
	var countMu sync.Mutex
	// The translators are built lazily; the screens below read them side by side.
	translator(false)
	for _, form := range PrefsSchema().Navigations {
		for _, binding := range []struct {
			name string
			keys map[action]string
		}{{"default keys", nil}, {"rebound keys", map[action]string{actInfo: "f3", actSave: "f4", actHelp: "ctrl+x", actCommand: "f6", actLock: "ctrl+l"}}} {
			// The screens of a form and binding run side by side: they only read the keymap, which stays bound
			// until the last of them is done.
			t.Run(form+"/"+binding.name, func(t *testing.T) {
				keepKeys(t)
				applyKeys(binding.keys)
				for _, screen := range keyLineScreens() {
					build := func(t *testing.T) *Model {
						m := screen.build(t)
						m.prefs.Navigation = form
						return m
					}
					t.Run(screen.name, func(t *testing.T) {
						t.Parallel()
						base := build(t)
						base.render()
						bars := assertBarTargetsClickAsKeys(t, base, build)
						countMu.Lock()
						barTargets.add(bars)
						countMu.Unlock()
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
								backButtons.Add(1)
							}
							if y > 0 {
								footerTargets.Add(1)
							}
							assertClickActsAsKey(t, build, x, y, firstKey(t, target.Action))
						}
						lists := assertListTargetsClickAsKeys(t, base, build)
						lists.add(assertWorkListTargetsClickAsKeys(t, base, build))
						countMu.Lock()
						listTargets.add(lists)
						countMu.Unlock()
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
				// The help and the explanation turn the wheel, which sets the process-wide wheelClamp, so they run
				// before the screens and dialogs, which wait until this function returns.
				for _, help := range clickHelps() {
					build := func(t *testing.T) *Model {
						m := help.build(t)
						m.prefs.Navigation = form
						// The targets of the open help come from the last drawing, which the form changes.
						m.render()
						return m
					}
					t.Run(help.name, func(t *testing.T) {
						assertHelpTargetsClickAsKeys(t, build, help.lists)
					})
				}
				// The command line turns the wheel as well. It stands over the screen in the default form, where it
				// covers the frame, and falls back to the workspace in every form.
				lines := [][2]int{{43, 30}}
				if form == PrefsSchema().DefaultNavigation {
					lines = append(lines, [2]int{140, 40})
				}
				for _, size := range lines {
					t.Run(fmt.Sprintf("command line %dx%d", size[0], size[1]), func(t *testing.T) {
						assertCommandLineTargetsClickAsKeys(t, form, size[0], size[1])
					})
				}
				if form == PrefsSchema().DefaultNavigation {
					t.Run("explanation", assertExplanationTargetsClickAsKeys)
				}
				// The picker turns the wheel too. Over the screen it hides the frame of the form, so it stands
				// there in the default form alone; in the workspace it stands under the frame of every form.
				for _, size := range [][2]int{{100, 40}, {minimumWidth, minimumHeight}} {
					if size[0] > minimumWidth && form != PrefsSchema().DefaultNavigation {
						continue
					}
					t.Run(fmt.Sprintf("value picker %dx%d", size[0], size[1]), func(t *testing.T) {
						assertPickerTargetsClickAsKeys(t, form, size[0], size[1])
					})
				}
				// A popup covers the frame, so it is tested in the default form; the bar and the workspace stand
				// below the frame of every form.
				for _, date := range clickLogDates(form) {
					if date.popup && form != PrefsSchema().DefaultNavigation {
						continue
					}
					t.Run(date.name, func(t *testing.T) {
						t.Parallel()
						assertLogDateTargetsClickAsKeys(t, date.build, date.calendar)
					})
				}
				for _, rows := range clickLogRows(form) {
					t.Run(rows.name, func(t *testing.T) {
						t.Parallel()
						assertLogRowTargetsClickAsKeys(t, rows.build)
					})
				}
				for _, bar := range clickLogBars(form) {
					if bar.popup && form != PrefsSchema().DefaultNavigation {
						continue
					}
					t.Run(bar.name, func(t *testing.T) {
						t.Parallel()
						assertLogBarTargetsClickAsKeys(t, bar.build)
					})
				}
				// The path list and the lists that are screens of their own turn the wheel too, so they run one
				// after the other; the lists over the screen stand there in the default form alone.
				for _, size := range [][2]int{{100, 40}, {minimumWidth, minimumHeight}} {
					t.Run(fmt.Sprintf("path list %dx%d", size[0], size[1]), func(t *testing.T) {
						assertPathTargetsClickAsKeys(t, form, size[0], size[1])
					})
				}
				for _, list := range choiceLists() {
					for _, size := range [][2]int{{100, 40}, {minimumWidth, minimumHeight}} {
						if size[0] > minimumWidth && form != PrefsSchema().DefaultNavigation {
							continue
						}
						t.Run(fmt.Sprintf("%s %dx%d", list.name, size[0], size[1]), func(t *testing.T) {
							assertChoiceTargetsClickAsKeys(t, list.open, list.screen, list.search, form, size[0], size[1])
						})
					}
				}
				for _, dialog := range clickDialogs() {
					build := func(t *testing.T) *Model {
						m := dialog.build(t)
						m.prefs.Navigation = form
						return m
					}
					t.Run(dialog.name, func(t *testing.T) {
						t.Parallel()
						assertDialogTargetsClickAsKeys(t, build)
					})
				}
				for _, dialog := range clickInputDialogs() {
					// An input dialog over the screen covers the frame of the form; in the workspace it stands under it.
					if form != PrefsSchema().DefaultNavigation && !strings.HasSuffix(dialog.name, " in the workspace") {
						continue
					}
					build := func(t *testing.T) *Model {
						m := dialog.build(t)
						m.prefs.Navigation = form
						return m
					}
					t.Run(dialog.name, func(t *testing.T) {
						t.Parallel()
						assertDialogTargetsClickAsKeys(t, build)
					})
				}
			})
		}
	}
	if backButtons.Load() == 0 || footerTargets.Load() == 0 {
		t.Fatalf("back buttons %d, footer targets %d: the screens drew no such control", backButtons.Load(), footerTargets.Load())
	}
	if listTargets.rows == 0 || listTargets.groups == 0 || listTargets.search == 0 || listTargets.free == 0 ||
		listTargets.ticks == 0 || listTargets.all == 0 {
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
	if base.section == sectionLogs {
		// The bar of the Logs has fields of its own, which clickLogBars tests.
		return n
	}
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
type listTargetCount struct{ rows, groups, search, free, ticks, all int }

func (c *listTargetCount) add(n listTargetCount) {
	c.rows, c.groups, c.search, c.free = c.rows+n.rows, c.groups+n.groups, c.search+n.search, c.free+n.free
	c.ticks, c.all = c.ticks+n.ticks, c.all+n.all
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

// assertWorkListTargetsClickAsKeys is assertListTargetsClickAsKeys for the kit list a screen shows in the
// workspace (the export questions, the findings): a row acts as the moves to it and, on the marked row, as
// enter; a tick box or the row of all as the moves to the row and, for a box, the toggle key; the search line
// as the search key.
func assertWorkListTargetsClickAsKeys(t *testing.T, base *Model, build func(t *testing.T) *Model) listTargetCount {
	t.Helper()
	var n listTargetCount
	list := base.workspaceList()
	if list == nil || list.Searching() {
		return n
	}
	const all, box = "\x00a", "\x00t"
	var rows []string
	for _, target := range base.listTargets {
		if target.Item == all {
			rows = append(rows, all)
		}
	}
	for _, id := range list.Shown() {
		rows = append(rows, string(id))
	}
	from := 0
	if id, ok := list.Selected(); ok {
		from = slices.Index(rows, string(id))
	}
	for _, target := range base.listTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		var keys []string
		if target.Action == actSearch && target.Item == "" {
			n.search++
			assertClickActsAsKey(t, build, x, y, firstKey(t, actSearch))
			continue
		}
		name, tick := strings.CutPrefix(target.Item, box)
		if target.Item == "" {
			continue
		}
		to := slices.Index(rows, name)
		if to < 0 || from < 0 {
			t.Errorf("the row target %q is no shown row of %q", name, rows)
			continue
		}
		move := firstKey(t, actDown)
		if to < from {
			move = firstKey(t, actUp)
		}
		keys = slices.Repeat([]string{move}, max(to-from, from-to))
		switch {
		case tick:
			n.ticks++
			keys = append(keys, firstKey(t, actSpace))
		case name == all:
			n.all++
		default:
			n.rows++
			if to == from {
				keys = append(keys, firstKey(t, actEnter))
			}
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

// clickLogDates are the date form of the Logs over the screen, with its calendar open, and in the workspace,
// whose keys stand in the footer, in the navigation form form. The builds of one test share a log, so each is
// quick. popup marks a form over the screen.
func clickLogDates(form string) []struct {
	name            string
	build           func(t *testing.T) *Model
	calendar, popup bool
} {
	var mu sync.Mutex
	logs := map[*testing.T]*logFixture{}
	open := func(w, h int, calendar bool) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			// The cases run side by side; each builds its log once.
			mu.Lock()
			f := logs[t]
			if f == nil {
				f = activityLog(t)
				logs[t] = f
			}
			mu.Unlock()
			m := f.model(false, f.localToday())
			m.prefs.Navigation = form
			m = sizedModel(m, w, h)
			m.logs.mode, m.logs.from, m.logs.to = logModeRange, f.localToday(), f.localToday()
			m.openLogDateDialog()
			if calendar {
				press(t, m, firstKey(t, actEnter))
			}
			m.render()
			return m
		}
	}
	return []struct {
		name            string
		build           func(t *testing.T) *Model
		calendar, popup bool
	}{{"log dates over the screen", open(140, 40, false), false, true}, {"log dates with the calendar", open(140, 40, true), true, true},
		{"log dates in the workspace", open(59, 19, false), false, false}}
}

// assertLogDateTargetsClickAsKeys requires each target of the date form to act as its keys: a field as tab to
// it and its calendar symbol then as enter, the marked day of the calendar and those beside it as the arrows to
// it and enter, a click outside as back, and every other target as the key of its action. With the calendar
// open, a click on anything but the calendar, the symbol of its field and the key line does nothing.
func assertLogDateTargetsClickAsKeys(t *testing.T, build func(t *testing.T) *Model, calendar bool) {
	t.Helper()
	base := build(t)
	form := logDateForm(base)
	if form == nil {
		t.Fatal("the date form is not open")
	}
	focused := string(form.Focused())
	next := firstKey(t, actNext)
	kinds := map[action]int{}
	for _, target := range base.modalTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y+(target.Rect.Height-1)/2
		var keys []string
		switch item := target.Item; {
		case target.Zone == kittui.ZoneOutside:
			keys = []string{firstKey(t, actBack)}
		case strings.HasPrefix(item, "\x00F"):
			if !calendar && strings.TrimPrefix(item, "\x00F") != focused {
				keys = []string{next}
			}
		case strings.HasPrefix(item, "\x00C"):
			own := strings.TrimPrefix(item, "\x00C") == focused
			switch {
			case calendar && own:
				keys = []string{firstKey(t, actBack)}
			case !calendar && own:
				keys = []string{firstKey(t, actEnter)}
			case !calendar:
				keys = []string{next, firstKey(t, actEnter)}
			}
		case strings.HasPrefix(item, "\x00D"):
			_, day, _ := strings.Cut(strings.TrimPrefix(item, "\x00D"), "\x00")
			to, err := time.ParseInLocation(logDateLayout, day, time.Local)
			mustNoError(t, err)
			from := form.Values()[form.Focused()].Time
			steps, arrow := int(to.Sub(from).Hours()/24+0.5), firstKey(t, actRight)
			if to.Before(from) {
				steps, arrow = int(from.Sub(to).Hours()/24+0.5), firstKey(t, actLeft)
			}
			if steps > 1 {
				// The days next to the marked one stand for the others, which keeps the test short.
				continue
			}
			keys = append(slices.Repeat([]string{arrow}, steps), firstKey(t, actEnter))
		case calendar && strings.HasPrefix(item, "\x00B") && !strings.Contains(item[2:], "\x00"):
			// A button of the form, not of the calendar.
		case target.Action != "":
			keys = []string{firstKey(t, target.Action)}
		default:
			continue
		}
		kinds[action(target.Item[:min(len(target.Item), 2)])+target.Action]++
		assertClickActsAsKey(t, build, x, y, keys...)
	}
	// A field, its calendar symbol and the two buttons; or a day and the buttons of the calendar.
	want := []action{"\x00F", "\x00C" + actEnter, "\x00B" + actSave, "\x00B" + actBack}
	if calendar {
		want = []action{"\x00D" + actEnter, "\x00B" + actPageUp, "\x00B" + actPageDown, "\x00B" + actHome}
	}
	for _, kind := range want {
		if kinds[kind] == 0 {
			t.Errorf("no target of kind %q; kinds %v", kind, kinds)
		}
	}
}

// clickLogBars are the bar of the Logs with its fields, with the values of the tool open, as the button of a
// narrow terminal, and its modal over the screen and in the workspace, in the navigation form form. The builds
// of one test share a log. popup marks the modal over the screen.
func clickLogBars(form string) []struct {
	name  string
	build func(t *testing.T) *Model
	popup bool
} {
	var mu sync.Mutex
	logs := map[*testing.T]*logFixture{}
	open := func(w, h int, keys ...string) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			// The cases run side by side; each builds its log once.
			mu.Lock()
			f := logs[t]
			if f == nil {
				f = activityLog(t)
				logs[t] = f
			}
			mu.Unlock()
			m := f.model(false, f.localToday())
			m.prefs.Navigation = form
			m = sizedModel(m, w, h)
			press(t, m, keys...)
			m.render()
			return m
		}
	}
	return []struct {
		name  string
		build func(t *testing.T) *Model
		popup bool
	}{{"log bar", open(180, 40), false}, {"log bar with values", open(180, 40, "f", "tab", "tab", "tab", "tab", "enter"), false},
		{"log bar as a button", open(80, 24), false}, {"log bar modal", open(80, 24, "f", "enter"), true},
		{"log bar modal in the workspace", open(40, 15, "f", "enter"), false}}
}

// clickLogRows are the rows of the Logs table in the navigation form form: as drawn, and with a kept search
// whose line is a target of its own in a smaller terminal. The builds of one test share a log.
func clickLogRows(form string) []struct {
	name  string
	build func(t *testing.T) *Model
} {
	var mu sync.Mutex
	logs := map[*testing.T]*logFixture{}
	// The summary keys get a log of three tools in one MCP session, so each list has lines to move over.
	open := func(w, h int, search string, keys ...string) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			mu.Lock()
			f := logs[t]
			if f == nil {
				f = activityLog(t)
				if slices.Contains(keys, "M") || slices.Contains(keys, "s") {
					f = newLogFixture(t)
					for i := range 3 {
						f.appendSession(sessionNew, "wiki.tool"+strconv.Itoa(i), "success", 10, 100)
					}
				}
				logs[t] = f
			}
			mu.Unlock()
			m := f.model(false, f.localToday())
			m.prefs.Navigation = form
			m = sizedModel(m, w, h)
			if search != "" {
				press(t, m, "/")
				typeText(t, m, search)
				press(t, m, "enter")
			}
			press(t, m, keys...)
			m.render()
			return m
		}
	}
	return []struct {
		name  string
		build func(t *testing.T) *Model
	}{{"log rows", open(140, 40, "")}, {"log rows with a search", open(80, 24, "wiki")},
		{"log summary", open(140, 40, "", "s")}, {"log sessions", open(80, 24, "", "M")}}
}

// assertLogRowTargetsClickAsKeys requires each target the list of the Logs drew to act as its keys while the
// rows have the focus: a row as the moves to it, and the marked row as enter, which opens its entry; the
// search line as the search key.
func assertLogRowTargetsClickAsKeys(t *testing.T, build func(t *testing.T) *Model) {
	t.Helper()
	base := build(t)
	list := base.logs.list
	if base.logs.stats {
		list = base.summaryList()
	}
	shown := list.Shown()
	at, _ := list.Selected()
	from := slices.Index(shown, at)
	rows, search := 0, 0
	for _, target := range base.listTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		var keys []string
		switch {
		case target.Item != "":
			to := slices.Index(shown, kittui.ItemID(target.Item))
			if to < 0 || from < 0 {
				t.Errorf("the row target %q is no shown row", target.Item)
				continue
			}
			rows++
			move := firstKey(t, actDown)
			if to < from {
				move = firstKey(t, actUp)
			}
			keys = slices.Repeat([]string{move}, max(to-from, from-to))
			if to == from {
				keys = append(keys, firstKey(t, actEnter))
			}
		case target.Action == actSearch:
			search++
			keys = []string{firstKey(t, actSearch)}
		default:
			continue
		}
		assertClickActsAsKey(t, build, x, y, keys...)
	}
	if rows < 2 {
		t.Errorf("%d row targets were tested", rows)
	}
	if want := base.logs.list.Query() != ""; (search > 0) != want {
		t.Errorf("%d search line targets with query %q", search, base.logs.list.Query())
	}
}

// assertLogBarTargetsClickAsKeys requires each target of the bar of the Logs to act as its keys from the rows:
// the bar as f, a field as f, tab to it and enter, the button as f and enter, a value of the open list as the
// arrows to it and enter; in the modal a field as down to it, its value as down to it and right, and a key of
// the key line or the footer as that key.
func assertLogBarTargetsClickAsKeys(t *testing.T, build func(t *testing.T) *Model) {
	t.Helper()
	base := build(t)
	bar := base.logs.bar
	fields := bar.Fields()
	at := func(id string) int {
		return slices.IndexFunc(fields, func(f kittui.FilterField) bool { return string(f.ID) == id })
	}
	repeat := func(key string, n int) []string { return slices.Repeat([]string{key}, n) }
	filter, next, enter, down := firstKey(t, actFilter), firstKey(t, actNext), firstKey(t, actEnter), firstKey(t, actDown)
	targets := base.modalTargets
	if base.modal == nil {
		targets = base.barTargets
	}
	if len(targets) == 0 {
		t.Fatal("the bar has no target")
	}
	// With the values open, only the values are tested; a field then opens its own values instead.
	listOpen := base.modal == nil && bar.Open()
	tested := 0
	for _, target := range targets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		var keys []string
		switch item := target.Item; {
		case listOpen && !strings.HasPrefix(item, "\x00o"):
			continue
		case strings.HasPrefix(item, "\x00mf"):
			keys = repeat(down, at(strings.TrimPrefix(item, "\x00mf")))
		case strings.HasPrefix(item, "\x00mv"):
			keys = append(repeat(down, at(strings.TrimPrefix(item, "\x00mv"))), firstKey(t, actRight))
		case strings.HasPrefix(item, "\x00f"):
			keys = append(append([]string{filter}, repeat(next, at(strings.TrimPrefix(item, "\x00f")))...), enter)
		case item == "\x00b":
			keys = []string{filter, enter}
		case strings.HasPrefix(item, "\x00o"):
			id, value, _ := strings.Cut(strings.TrimPrefix(item, "\x00o"), "\x00")
			f := fields[at(id)]
			j := slices.IndexFunc(f.Options, func(c kittui.Choice) bool { return string(c.ID) == value })
			from := slices.IndexFunc(f.Options, func(c kittui.Choice) bool { return c.ID == f.Selected })
			keys = append(repeat(down, j-from), enter)
		case target.Action != "":
			keys = []string{firstKey(t, target.Action)}
		case item == "":
			// The free area of the bar, at its right end beside the fields.
			x, keys = target.Rect.X+target.Rect.Width-1, []string{filter}
		default:
			t.Errorf("the target %q is of no known kind", item)
			continue
		}
		tested++
		assertClickActsAsKey(t, build, x, y, keys...)
	}
	if tested < 2 {
		t.Errorf("%d targets of the bar were tested", tested)
	}
}

// assertCommandLineTargetsClickAsKeys requires each target of the command line opened by its key to act as
// keys: a row as down up to that row and enter, a key of its key line or the footer as that key, and a click
// outside its frame as the back key. A notch of the wheel down over the rows acts as down.
func assertCommandLineTargetsClickAsKeys(t *testing.T, form string, w, h int) {
	t.Helper()
	build := func(t *testing.T) *Model {
		m := sizedModel(groupModel(t), w, h)
		m.prefs.Navigation = form
		openSectionByName(t, m, sectionServices)
		press(t, m, firstKey(t, actCommand))
		m.render()
		return m
	}
	base := build(t)
	if base.commandLine() == nil {
		t.Fatal("the command line is not open")
	}
	matches := base.commandLine().Matches()
	rows, keys, outside := 0, map[action]bool{}, false
	var row kittui.Target
	for _, target := range base.modalTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		switch {
		case target.Zone == kittui.ZoneRows:
			i := slices.IndexFunc(matches, func(c kittui.Command) bool {
				return c.Action == target.Action && c.Area == target.Area && c.Tab == target.Tab
			})
			if i < 0 {
				t.Fatalf("the row target %+v is no match", target)
			}
			rows++
			row = target
			assertClickActsAsKey(t, build, x, y, append(slices.Repeat([]string{firstKey(t, actDown)}, i), firstKey(t, actEnter))...)
		case target.Zone == kittui.ZoneOutside:
			outside = true
			assertClickActsAsKey(t, build, x, y+target.Rect.Height/2, firstKey(t, actBack))
		case target.Action == actEnter || target.Action == actBack:
			keys[target.Action] = true
			assertClickActsAsKey(t, build, x, y, firstKey(t, target.Action))
		}
	}
	if rows == 0 || !keys[actEnter] || !keys[actBack] {
		t.Errorf("%d rows are targets, the keys %v", rows, keys)
	}
	if over := base.termWidth >= 44; outside != over {
		t.Errorf("the screen outside the frame is a target %v, want %v", outside, over)
	}
	pressed, wheeled := build(t), build(t)
	press(t, pressed, firstKey(t, actDown))
	wheelEvent(t, wheeled, row.Rect.X+row.Rect.Width/2, row.Rect.Y, tea.MouseWheelDown)
	if want, got := clickState(pressed), clickState(wheeled); want != got {
		t.Errorf("a notch of the wheel over the rows does not act as down:\nkey:\n%s\nwheel:\n%s", want, got)
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

// assertPickerTargetsClickAsKeys requires each target of the picker of the effects, over the form or in the
// workspace with its keys in the footer, to act as its key: a row as the moves to it, the marked row as enter,
// a tick box as the moves and space, a button or a key as its key, and a click beside the frame as esc. A
// click on the search line, which takes the typing anyway, changes nothing. A notch of the wheel over the rows
// acts as down.
func assertPickerTargetsClickAsKeys(t *testing.T, form string, width, height int) {
	t.Helper()
	build := func(t *testing.T) *Model {
		m := connectionForm(t, 100)
		m.prefs.Navigation = form
		selectAccess(t, m, accessEffects)
		focusField(t, m, effectsLabel)
		press(t, m, "enter")
		m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m.render()
		return m
	}
	base := build(t)
	if !valuePickerOpen(base) {
		t.Fatal("the picker is not open")
	}
	marked, _ := base.valuePick.list.Selected()
	moves := func(item string) []string {
		at, _ := strconv.Atoi(item)
		from, _ := strconv.Atoi(string(marked))
		if at < from {
			return slices.Repeat([]string{firstKey(t, actUp)}, from-at)
		}
		return slices.Repeat([]string{firstKey(t, actDown)}, at-from)
	}
	seen := map[string]bool{}
	for _, target := range base.modalTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		item, tick := strings.CutPrefix(target.Item, "\x00t")
		switch {
		case target.Zone == kittui.ZoneOutside:
			seen["outside"] = true
			assertClickActsAsKey(t, build, x, y+target.Rect.Height/2, firstKey(t, actBack))
		case target.Action == actSearch:
			seen["search"] = true
			assertClickActsAsKey(t, build, x, y)
		case tick:
			seen["tick"] = true
			assertClickActsAsKey(t, build, target.Rect.X+1, y, append(moves(item), firstKey(t, actSpace))...)
		case target.Action == actEnter && item != "" && !strings.HasPrefix(item, "\x00"):
			seen["row"] = true
			keys := moves(item)
			if item == string(marked) {
				keys = []string{firstKey(t, actEnter)}
			}
			assertClickActsAsKey(t, build, x, y, keys...)
		case target.Action != "":
			seen[string(target.Action)] = true
			assertClickActsAsKey(t, build, x, y, firstKey(t, target.Action))
		}
	}
	for _, want := range []string{"row", "tick", "search", string(actEnter), string(actBack), string(actSpace)} {
		if !seen[want] {
			t.Errorf("the picker at %dx%d has no target %q: %v", width, height, want, seen)
		}
	}
	if width > minimumWidth && !seen["outside"] {
		t.Errorf("the screen beside the picker is no target")
	}
	pressed, wheeled := build(t), build(t)
	press(t, pressed, firstKey(t, actDown))
	_, y := cellOf(t, base, "create")
	wheelEvent(t, wheeled, base.termWidth/2, y, tea.MouseWheelDown)
	if want, got := clickState(pressed), clickState(wheeled); want != got {
		t.Errorf("a notch of the wheel does not act as down:\nkey:\n%s\nwheel:\n%s", want, got)
	}
}

// pathListModel is the path list of a connection with three paths, the second marked, at width by height.
func pathListModel(t *testing.T, width, height int) *Model {
	t.Helper()
	m, _ := toolsModel(t, wikiRegistry(t), map[string]config.Connection{
		"wiki": {Service: "wiki", Credential: "reader", Paths: []string{"~/a", "~/b", "~/c"}}})
	sizedModel(m, 140, 40)
	openEntryForm(t, m, sectionConnections, "wiki")
	openPathList(t, m)
	press(t, m, "down")
	return sizedModel(m, width, height)
}

// assertPathTargetsClickAsKeys requires each row of the path list to act as its key, the moves to it and enter
// on the marked one, which opens the dialog of its path, and the free area below the rows to do nothing. A
// notch of the wheel over the rows acts as down.
func assertPathTargetsClickAsKeys(t *testing.T, form string, width, height int) {
	t.Helper()
	build := func(t *testing.T) *Model {
		m := pathListModel(t, width, height)
		m.prefs.Navigation = form
		m.render()
		return m
	}
	base := build(t)
	from := base.pathList.cursor()
	rows := 0
	var row kittui.Target
	for _, target := range base.listTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		at, err := strconv.Atoi(target.Item)
		if err != nil {
			continue
		}
		rows, row = rows+1, target
		keys := slices.Repeat([]string{firstKey(t, actDown)}, max(at-from, 0))
		keys = append(keys, slices.Repeat([]string{firstKey(t, actUp)}, max(from-at, 0))...)
		if at == from {
			keys = []string{firstKey(t, actEnter)}
		}
		assertClickActsAsKey(t, build, x, y, keys...)
	}
	if rows != len(base.pathList.all) {
		t.Fatalf("the path list at %dx%d has %d row targets for %d paths", width, height, rows, len(base.pathList.all))
	}
	pressed, wheeled := build(t), build(t)
	press(t, pressed, firstKey(t, actDown))
	wheelEvent(t, wheeled, row.Rect.X+row.Rect.Width/2, row.Rect.Y, tea.MouseWheelDown)
	if want, got := clickState(pressed), clickState(wheeled); want != got {
		t.Errorf("a notch of the wheel does not act as down:\nkey:\n%s\nwheel:\n%s", want, got)
	}
}

// choiceLists open the lists that are screens of their own: the provider table, the template question, its
// entries and the version list. search says whether the list has a search line.
func choiceLists() []struct {
	name   string
	open   func(t *testing.T) *Model
	screen screen
	search bool
} {
	return []struct {
		name   string
		open   func(t *testing.T) *Model
		screen screen
		search bool
	}{
		{"provider table", func(t *testing.T) *Model {
			m := sizedModel(groupModel(t), 100, 40)
			openSectionByName(t, m, sectionServices)
			pressNew(t, m)
			focusField(t, m, providerLabel)
			press(t, m, "enter")
			return m
		}, screenProviders, true},
		{"template question", func(t *testing.T) *Model {
			m, _ := templateModel(t, "alpha", "beta")
			openSectionByName(t, m, sectionConnections)
			m.screen = screenList
			press(t, m, "n")
			return m
		}, screenTemplate, false},
		{"template entries", func(t *testing.T) *Model {
			m, _ := templateModel(t, "alpha", "beta")
			openSectionByName(t, m, sectionConnections)
			m.screen = screenList
			press(t, m, "n", "down", "enter")
			return m
		}, screenTemplate, true},
		{"version list", func(t *testing.T) *Model {
			// The command line is reached by the key in force, which the rebound keys move.
			m := startedVersions(t, choosable())
			deliver(m, m.openVersions())
			return m
		}, screenVersions, true},
	}
}

// assertChoiceTargetsClickAsKeys requires each target of a list that is a screen of its own, over the screen
// it was opened from or in the workspace with its keys in the footer, to act as its key: a row as the moves
// to it, the marked row as enter, a button or a key as its key, and a click beside the frame as esc. A click
// on the search line, which takes the typing anyway, changes nothing. A notch of the wheel over the rows acts
// as down.
func assertChoiceTargetsClickAsKeys(t *testing.T, open func(t *testing.T) *Model, on screen, search bool, form string, width, height int) {
	t.Helper()
	build := func(t *testing.T) *Model {
		m := open(t)
		m.prefs.Navigation = form
		m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m.render()
		return m
	}
	base := build(t)
	if !choiceOpen(base, on) {
		t.Fatalf("the list is not open: screen %v", base.screen)
	}
	list, _, _ := base.screenChoice()
	marked, _ := list.Selected()
	from, _ := strconv.Atoi(string(marked))
	seen := map[string]bool{}
	var row kittui.Target
	for _, target := range base.modalTargets {
		x, y := target.Rect.X+(target.Rect.Width-1)/2, target.Rect.Y
		at, err := strconv.Atoi(target.Item)
		switch {
		case target.Zone == kittui.ZoneOutside:
			seen["outside"] = true
			assertClickActsAsKey(t, build, x, y+target.Rect.Height/2, firstKey(t, actBack))
		case target.Action == actSearch:
			seen["search"] = true
			assertClickActsAsKey(t, build, x, y)
		case err == nil:
			seen["row"], row = true, target
			keys := slices.Repeat([]string{firstKey(t, actDown)}, max(at-from, 0))
			keys = append(keys, slices.Repeat([]string{firstKey(t, actUp)}, max(from-at, 0))...)
			if at == from {
				keys = []string{firstKey(t, actEnter)}
			}
			assertClickActsAsKey(t, build, x, y, keys...)
		case target.Action != "":
			seen[string(target.Action)] = true
			assertClickActsAsKey(t, build, x, y, firstKey(t, target.Action))
		}
	}
	want := []string{"row", string(actEnter), string(actBack)}
	if search {
		want = append(want, "search")
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("the list at %dx%d has no target %q: %v", width, height, w, seen)
		}
	}
	if width > minimumWidth && !seen["outside"] {
		t.Errorf("the screen beside the list is no target")
	}
	pressed, wheeled := build(t), build(t)
	press(t, pressed, firstKey(t, actDown))
	wheelEvent(t, wheeled, row.Rect.X+row.Rect.Width/2, row.Rect.Y, tea.MouseWheelDown)
	if want, got := clickState(pressed), clickState(wheeled); want != got {
		t.Errorf("a notch of the wheel does not act as down:\nkey:\n%s\nwheel:\n%s", want, got)
	}
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
