package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// vaultCases are the vault states the header shows, each with the word it must name. A warning stands at
// every width; the other marks give way to the keys of the header where the width is short.
func vaultCases(t *testing.T) []struct {
	name    string
	m       *Model
	words   []string
	warning bool
} {
	t.Helper()
	none, _, _ := newModel(t)
	locked, _ := newEncryptedVaultModel(t, filepath.Join(t.TempDir(), "qconnect"), "hunter2", false)
	unlocked, _ := newEncryptedVaultModel(t, filepath.Join(t.TempDir(), "qconnect"), "hunter2", true)
	return []struct {
		name    string
		m       *Model
		words   []string
		warning bool
	}{
		{"no vault", none, nil, false},
		{"unencrypted", unencryptedVaultModel(t), []string{"vault unencrypted"}, true},
		{"locked", locked, []string{"vault locked"}, true},
		{"unlocked", unlocked, []string{"vault unlocked", "admin"}, false},
	}
}

// drawnFrame is the frame of the current screen around an empty workspace, line by line, as the kit draws it.
func drawnFrame(m *Model) []string {
	m.layoutWorkspace()
	view, _ := m.kitFrame().Render(styles.Styles, m.spec)
	return strings.Split(view, "\n")
}

// headerLine, tabLine and subTabLine are the first lines of the frame, footerText its last one or two.
func headerLine(m *Model) string { return drawnFrame(m)[0] }
func tabLine(m *Model) string    { return drawnFrame(m)[1] }
func subTabLine(m *Model) string { return drawnFrame(m)[2] }
func footerText(m *Model) string {
	lines := drawnFrame(m)
	return strings.Join(lines[m.top+m.height:], "\n")
}

// footerRows is the number of rows below the workspace, which the footer takes.
func footerRows(m *Model) int { return m.termHeight - m.top - m.height }

// The footer takes the last line of the screen, or the last two, at every size and in every vault state, and
// it names the help key; the workspace takes the rows between the frame's lines.
func TestFooterIsTheLastLineAtEverySize(t *testing.T) {
	sizes := []struct{ width, height int }{{minimumWidth, minimumHeight}, {79, 24}, {150, 42}}
	for _, vc := range vaultCases(t) {
		for _, size := range sizes {
			for _, scr := range []screen{screenNav, screenList} {
				t.Run(fmt.Sprintf("%s/%dx%d/%v", vc.name, size.width, size.height, scr), func(t *testing.T) {
					m := vc.m
					m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
					m.section, m.screen = sectionServices, scr
					view := m.render()
					assertViewFits(t, view, size.width, size.height)
					lines := strings.Split(view, "\n")
					if len(lines) != size.height {
						t.Fatalf("view has %d lines, want %d:\n%s", len(lines), size.height, view)
					}
					rows := footerRows(m)
					if rows < 1 || rows > 2 {
						t.Fatalf("the footer takes %d rows", rows)
					}
					if foot := strings.Join(lines[len(lines)-rows:], "\n"); !namesHelp(foot) {
						t.Errorf("the footer %q does not name the help key:\n%s", foot, view)
					}
				})
			}
		}
	}
}

// Below the smallest size the terminal shows the resize notice instead of a clipped frame.
func TestBelowTheSmallestSizeTheResizeNoticeStands(t *testing.T) {
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: minimumWidth, Height: minimumHeight - 1})
	if view := m.render(); !strings.Contains(view, tr(tResizeTitle)) {
		t.Errorf("no resize notice at %dx%d:\n%s", minimumWidth, minimumHeight-1, view)
	}
}

// Every section shows its content at the smallest size with one status note standing, in every vault state
// and both languages; the resize notice is for terminals below it.
func TestEverySectionFitsTheSmallestSizeWithAStatusNote(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		for _, vc := range vaultCases(t) {
			for s := section(0); s < sectionCount; s++ {
				t.Run(fmt.Sprintf("%s/%d", vc.name, s), func(t *testing.T) {
					m := vc.m
					height := minimumHeight
					m.Update(tea.WindowSizeMsg{Width: minimumWidth, Height: height})
					m.section, m.screen = s, screenNav
					m.status = tr(tCancelled)
					view := m.render()
					if strings.Contains(view, tr(tResizeTitle)) {
						t.Fatalf("the resize notice at %dx%d:\n%s", minimumWidth, height, view)
					}
					assertViewFits(t, view, minimumWidth, height)
					if !strings.Contains(view, tr(tCancelled)) {
						t.Errorf("the status note is missing:\n%s", view)
					}
				})
			}
		}
	})
}

// The header names every vault and admin state, and the workspace keeps the rows the frame leaves it at each
// change of it. A warning stands at the smallest width too.
func TestHeaderNamesEveryStateAndTheHeightFollows(t *testing.T) {
	for _, vc := range vaultCases(t) {
		for _, width := range []int{minimumWidth, 79, 150} {
			t.Run(fmt.Sprintf("%s/%d", vc.name, width), func(t *testing.T) {
				m := vc.m
				m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
				header := strings.Join(strings.Fields(headerLine(m)), " ")
				for _, word := range vc.words {
					if (vc.warning || width > minimumWidth) && !strings.Contains(header, word) {
						t.Errorf("header %q lacks %q", header, word)
					}
				}
				if !strings.Contains(header, "Qatlas") {
					t.Errorf("header %q lacks the name", header)
				}
				if rows := footerRows(m); rows < 1 || rows > 2 {
					t.Errorf("workspace height %d leaves %d rows for the footer", m.height, rows)
				}
			})
		}
	}

	unlocked, _ := newEncryptedVaultModel(t, filepath.Join(t.TempDir(), "qconnect"), "hunter2", true)
	for _, width := range []int{79, 150} {
		unlocked.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		for _, step := range []struct {
			name string
			do   func()
			want string
		}{
			{"admin session starts", unlocked.startAdminSession, "admin 10m left"},
			{"admin session ends", unlocked.endAdminSession, "admin off"},
			{"admin asks every time", func() {
				unlocked.adminTimeoutValue, unlocked.adminTimeoutRead = 0, true
			}, "admin: confirm each change"},
		} {
			step.do()
			unlocked.Update(vaultTickMsg{})
			header := strings.Join(strings.Fields(headerLine(unlocked)), " ")
			if !strings.Contains(header, step.want) {
				t.Errorf("width %d, %s: header %q lacks %q", width, step.name, header, step.want)
			}
			view := unlocked.render()
			lines := strings.Split(view, "\n")
			if rows := footerRows(unlocked); len(lines) != 30 || rows < 1 || rows > 2 || !namesHelp(strings.Join(lines[30-rows:], "\n")) {
				t.Errorf("width %d, %s: the footer is not at the end:\n%s", width, step.name, view)
			}
		}
		unlocked.RefreshAdminTimeout()
	}
}

// The header names the keys of help and the command line as they are bound.
func TestHeaderNamesTheBoundKeys(t *testing.T) {
	m, _, _ := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	if got := headerLine(m); !strings.Contains(got, "[?] help") || !strings.Contains(got, "[:] cmd") {
		t.Fatalf("header %q lacks the default keys", got)
	}
	rebind(t, actHelp, "ctrl+x")
	rebind(t, actCommand, "f3")
	got := headerLine(m)
	if !strings.Contains(got, "[ctrl+x] help") || !strings.Contains(got, "[F3] cmd") || strings.Contains(got, "[?]") {
		t.Errorf("header %q does not follow the keymap", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 30})
	if got := headerLine(m); strings.Contains(got, "cmd") {
		t.Errorf("header %q keeps the hints although they do not fit", got)
	}
}

// The help and command keys stand in the header only where they act: a text field takes ? and : as text.
func TestHeaderKeysStandWhereTheyAct(t *testing.T) {
	m := sizedModel(groupModel(t), 140, 40)
	openSectionByName(t, m, sectionConnections)
	if got := ansi.Strip(headerLine(m)); !strings.Contains(got, "[?] help") || !strings.Contains(got, "[:] cmd") {
		t.Fatalf("list header %q lacks the keys", got)
	}
	pressNew(t, m)
	if !m.typing() {
		t.Fatalf("the new form does not start on a text field")
	}
	if got := ansi.Strip(headerLine(m)); strings.Contains(got, "[?]") || strings.Contains(got, "[:]") {
		t.Errorf("header %q names keys that type text in the field", got)
	}
}

// The footer takes a second row before it gives up groups, then gives them up navigation first, never the
// help key, and never exceeds the width.
func TestFooterGivesUpGroupsByRankAndKeepsHelp(t *testing.T) {
	m, _, _ := newModel(t)
	text := "{up}/{down} move · {enter} edit · {new} new · {filter} filter · {back} back · {command} cmds · {help} help · {quit} quit"
	for width := 120; width >= 8; width-- {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		spec := m.spec
		spec.Footer = m.keyHints(text)
		view, _ := m.kitFrame().Render(styles.Styles, spec)
		lines := strings.Split(ansi.Strip(view), "\n")
		foot := strings.Join(lines[len(lines)-2:], "\n")
		for _, line := range lines[len(lines)-2:] {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("width %d: footer is %d cells: %q", width, w, line)
			}
		}
		if !namesHelp(foot) {
			t.Fatalf("width %d: footer %q lost the help key", width, foot)
		}
		if strings.Contains(foot, "↑/↓ move") {
			for _, group := range []string{"enter edit", "n new", "f filter", "esc back", ": cmds", "q quit"} {
				if !strings.Contains(foot, group) {
					t.Errorf("width %d: footer %q keeps navigation and gave up %q", width, foot, group)
				}
			}
		}
		if width == 40 && !strings.Contains(foot, "esc back") {
			t.Errorf("40 cells: footer %q lost esc back", foot)
		}
	}
}

// What the last action reports stays on screen next to the footer.
func TestStatusAndErrorStayVisible(t *testing.T) {
	for _, size := range []struct{ width, height int }{{minimumWidth, minimumHeight}, {79, 24}} {
		m, _, _ := newModel(t)
		m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		openSectionByName(t, m, sectionServices)
		m.status = "saved"
		if view := m.render(); !strings.Contains(view, "saved") {
			t.Errorf("%dx%d: the status is gone:\n%s", size.width, size.height, view)
		}
		m.status, m.fail = "", "something failed"
		view := m.render()
		if flat := flatText(view); !strings.Contains(flat, "something failed") {
			t.Errorf("%dx%d: the error is gone:\n%s", size.width, size.height, view)
		}
		if lines := strings.Split(view, "\n"); !namesHelp(strings.Join(lines[len(lines)-footerRows(m):], "\n")) {
			t.Errorf("%dx%d: the footer is gone under the error:\n%s", size.width, size.height, view)
		}
	}
}

var colourCode = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// hasColour reports whether text carries an SGR code that selects a colour.
func hasColour(text string) bool {
	for _, code := range colourCode.FindAllString(text, -1) {
		for _, p := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(code, "\x1b["), "m"), ";") {
			if p == "38" || p == "48" || (len(p) == 2 && (p[0] == '3' || p[0] == '4' || p[0] == '9') && p[1] <= '7') {
				return true
			}
		}
	}
	return false
}

// Without colour the header, the tab line and the footer read as text: the active tab stays marked, the
// vault state keeps its word, and the keys keep their names.
func TestFrameReadsWithoutColour(t *testing.T) {
	before := currentProfile()
	prior := styles
	t.Cleanup(func() { setProfile(before); setTheme(prior) })
	setProfile(kittui.ProfileMono)
	applyTheme(kittui.ThemeTerminal)

	m, _ := newEncryptedVaultModel(t, filepath.Join(t.TempDir(), "qconnect"), "hunter2", true)
	m.Update(tea.WindowSizeMsg{Width: 220, Height: 30})
	m.section, m.screen = sectionServices, screenList
	view := m.render()
	lines := strings.Split(view, "\n")
	header, tabs, footer := lines[0], lines[1], strings.Join(lines[len(lines)-footerRows(m):], "\n")
	for name, line := range map[string]string{"header": header, "tab line": tabs, "footer": footer} {
		if hasColour(line) {
			t.Errorf("%s carries a colour under NO_COLOR: %q", name, line)
		}
	}
	for _, want := range []string{"vault unlocked", "admin off", "[?] help"} {
		if !strings.Contains(ansi.Strip(header), want) {
			t.Errorf("header %q lacks %q", header, want)
		}
	}
	if !strings.Contains(ansi.Strip(tabs), "[2 Library]") {
		t.Errorf("tab line %q does not mark the active tab", tabs)
	}
	// The list of services is empty, so its search key, which stands in the filter bar of the list, is left out.
	if plain := ansi.Strip(footer); !strings.Contains(plain, "? help") || !strings.Contains(plain, "n new") || strings.Contains(plain, "/ search") {
		t.Errorf("footer %q lacks its keys", footer)
	}
	if !strings.Contains(ansi.Strip(header), "●") {
		t.Errorf("header %q has no status mark", header)
	}
}

// The header names the product in full at a usual width and gives way to the short name where the name and
// the keys at its right end do not fit beside each other, staying one row.
func TestHeaderNameFollowsTheWidth(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		for _, vc := range vaultCases(t) {
			if len(vc.words) == 0 {
				continue
			}
			t.Run(vc.name, func(t *testing.T) {
				m := vc.m
				for _, width := range []int{80, 150} {
					m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
					if row := ansi.Strip(headerLine(m)); !strings.Contains(row, tr(tHeaderTitle)) {
						t.Errorf("width %d: header %q lacks %q", width, row, tr(tHeaderTitle))
					}
				}
				m.Update(tea.WindowSizeMsg{Width: minimumWidth, Height: minimumHeight})
				m.termWidth = lipgloss.Width(tr(tHeaderTitle)) + 3
				line := headerLine(m)
				if ansi.StringWidth(line) > m.termWidth {
					t.Fatalf("header %q is not one row within %d cells", line, m.termWidth)
				}
				if row := ansi.Strip(line); strings.Contains(row, tr(tHeaderTitle)) || !strings.Contains(row, tr(tHeaderTitleShort)) {
					t.Errorf("header %q does not show the short name %q", row, tr(tHeaderTitleShort))
				}
			})
		}
	})
}

// The warning about the vault stands in a narrow header too, before the back button and the keys of the
// header: it is what the person must not miss.
func TestTheVaultWarningStaysInANarrowHeader(t *testing.T) {
	for _, vc := range vaultCases(t) {
		if !vc.warning {
			continue
		}
		t.Run(vc.name, func(t *testing.T) {
			m := vc.m
			m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
			openSectionByName(t, m, sectionConnections)
			pressNew(t, m)
			press(t, m, "tab")
			for width := minimumWidth; width <= 100; width++ {
				m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
				row := ansi.Strip(headerLine(m))
				if !strings.Contains(row, vc.words[0]) {
					t.Fatalf("width %d: header %q lacks the warning %q", width, row, vc.words[0])
				}
			}
		})
	}
}

// From the smallest width on the header is one row in every vault state, with the admin text that asks for
// every change too, in both languages, and it never names the config path.
func TestHeaderIsOneRowWithoutThePathAtTheSmallestWidth(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		for _, vc := range vaultCases(t) {
			for _, each := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/each-change=%v", vc.name, each), func(t *testing.T) {
					m := vc.m
					m.RefreshAdminTimeout()
					if each {
						m.adminTimeoutValue = 0
					}
					for _, width := range []int{minimumWidth, minimumWidth + 1, 60} {
						m.Update(tea.WindowSizeMsg{Width: width, Height: minimumHeight})
						line := headerLine(m)
						if ansi.StringWidth(line) > width {
							t.Fatalf("width %d: header %q is not one row within the terminal", width, line)
						}
						if row := ansi.Strip(line); strings.Contains(row, "config.yaml") || !strings.Contains(row, "Qatlas") {
							t.Errorf("width %d: header %q", width, row)
						}
						if vc.warning {
							if row := ansi.Strip(line); !strings.Contains(row, "●") {
								t.Errorf("width %d: header %q lacks the vault mark", width, row)
							}
						}
					}
				})
			}
		}
	})
}
