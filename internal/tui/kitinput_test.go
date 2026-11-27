package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

const inputCanary = "canary-passphrase-5c21e8"

// inputDialogCase is a dialog with an input: how to open it in a terminal of 140 by 40 cells, and the error
// enter gives on an empty input, "" where an empty input declines.
type inputDialogCase struct {
	name  string
	build func(t *testing.T) *Model
	empty textID
}

// inputDialogCases are the masked dialogs, each opened the way its screen opens it.
func inputDialogCases() []inputDialogCase {
	locked := func(open func(m *Model)) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			m, _ := newEncryptedVaultModel(t, filepath.Join(t.TempDir(), "qconnect"), "hunter2", false)
			sizedModel(m, 140, 40)
			openSectionByName(t, m, sectionServices)
			open(m)
			return m
		}
	}
	offer := func(t *testing.T) *Model {
		m, _ := startServiceForm(t)
		sizedModel(m, 140, 40)
		back := func() tea.Cmd { m.screen = screenForm; m.status = tr(tCancelled); return nil }
		m.openVaultOfferFor(tVaultOfferTitle, vaultFirstSecretHint(), true, true,
			func(func(string) (string, error)) tea.Cmd { return back() }, back)
		return m
	}
	return []inputDialogCase{
		{"unlock dialog", locked(func(m *Model) { m.openVaultUnlockPrompt(m.screen) }), tPassphraseEmpty},
		{"admin dialog", locked(func(m *Model) { m.openAdminPrompt(true, m.screen, func() tea.Cmd { return nil }) }),
			tPassphraseEmpty},
		{"secret dialog", func(t *testing.T) *Model {
			m, _ := startServiceForm(t)
			sizedModel(m, 140, 40)
			m.askSecret("key")
			return m
		}, tSecretNothingWasTypedSoNothing},
		{"vault offer", offer, ""},
		{"vault offer repeated", func(t *testing.T) *Model {
			m := offer(t)
			m.Update(tea.PasteMsg{Content: inputCanary})
			pump(t, m, "enter")
			if m.vaultOffer == nil || !m.vaultOffer.confirming {
				t.Fatal("enter did not ask to repeat the passphrase")
			}
			return m
		}, tPassphraseMismatch},
		{"export passphrase", func(t *testing.T) *Model {
			m, _, _ := newModel(t)
			sizedModel(m, 140, 40)
			m.openExport()
			m.export.step = exportMode
			m.askExportPassphrase()
			return m
		}, tExportPassEmpty},
		{"export passphrase repeated", func(t *testing.T) *Model {
			m, _, _ := newModel(t)
			sizedModel(m, 140, 40)
			m.openExport()
			m.export.step = exportMode
			m.askExportPassphrase()
			m.Update(tea.PasteMsg{Content: inputCanary})
			pump(t, m, "enter")
			if !m.export.confirming {
				t.Fatal("enter did not ask to repeat the passphrase")
			}
			return m
		}, tPassphraseMismatch},
		{"import passphrase", func(t *testing.T) *Model {
			m, _, _ := newModel(t)
			sizedModel(m, 140, 40)
			m.openImport()
			m.askImportPassphrase()
			return m
		}, tExportPassEmpty},
	}
}

// pathDialogCases are the dialogs that ask for a path, each with the one suggestion of an empty input: the
// root, whose entries stay the same between two models.
func pathDialogCases() []inputDialogCase {
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	return []inputDialogCase{
		{"path dialog", func(t *testing.T) *Model { return pathDialogIn(t, root) }, tPathAPathMustNotBe},
		{"export file dialog", func(t *testing.T) *Model {
			v, _ := plainExportModel(t)
			sizedModel(v.m, 140, 40)
			v.m.startDir = root
			v.m.openExport()
			v.m.export.step = exportMode
			v.m.askExportTarget()
			v.m.export.target = ""
			return v.m
		}, tExportNameFile},
		{"import file dialog", func(t *testing.T) *Model {
			m, _, _ := newModel(t)
			sizedModel(m, 140, 40)
			m.startDir = root
			m.openImport()
			return m
		}, tImportNameFile},
	}
}

// clickInputDialogs are the dialogs of inputDialogCases without their repeated steps, which draw the same
// targets, the dialogs of pathDialogCases and the result of a connection test, each over the screen and in
// the workspace of a terminal too narrow for that.
func clickInputDialogs() []inputDialogCase {
	var out []inputDialogCase
	test := inputDialogCase{"connection test result", func(t *testing.T) *Model {
		m, _, _ := newModel(t)
		sizedModel(m, 140, 40)
		seedConnections(t, m, "docs")
		openSectionByName(t, m, sectionConnections)
		m.testName, m.testClass = "docs", provider.ClassAuth
		return m
	}, ""}
	for _, c := range append(append(inputDialogCases(), pathDialogCases()...), test) {
		if strings.HasSuffix(c.name, " repeated") {
			continue
		}
		for _, size := range []struct {
			name          string
			width, height int
		}{{"", 140, 40}, {" in the workspace", minimumWidth + 3, 24}} {
			build := c.build
			out = append(out, inputDialogCase{c.name + size.name, func(t *testing.T) *Model {
				return sizedModel(build(t), size.width, size.height)
			}, c.empty})
		}
	}
	return out
}

// popupShown reports whether the open dialog stands over the screen, which a click beside it answers.
func popupShown(m *Model) bool {
	return slices.ContainsFunc(m.modalTargets, func(t kittui.Target) bool { return t.Zone == kittui.ZoneOutside })
}

// Every dialog with an input stands over the screen where 36 cells of text fit beside its frame, and fills
// the workspace below that, with its answers in the footer; it fits the terminal at every size and in every
// navigation form.
func TestEveryInputDialogFitsAsPopupOrInTheWorkspace(t *testing.T) {
	for _, c := range append(inputDialogCases(), pathDialogCases()...) {
		for i, size := range [][2]int{{40, 15}, {59, 19}, {60, 20}, {80, 24}, {150, 42}} {
			m := sizedModel(c.build(t), size[0], size[1])
			forms := PrefsSchema().Navigations
			m.prefs.Navigation = forms[i%len(forms)]
			view := m.render()
			assertViewFits(t, view, size[0], size[1])
			if m.question == nil || m.modal != m.question || m.question.Input() != kittui.InputText {
				t.Errorf("%s %dx%d: the input is no dialog of the kit", c.name, size[0], size[1])
				continue
			}
			if popup := popupShown(m); popup != (size[0]-8 >= 36) {
				t.Errorf("%s %dx%d: popup %v:\n%s", c.name, size[0], size[1], popup, ansi.Strip(view))
			}
			if !strings.Contains(ansi.Strip(view), "esc ") {
				t.Errorf("%s %dx%d: the esc answer is not shown:\n%s", c.name, size[0], size[1], ansi.Strip(view))
			}
		}
	}
}

// Enter on an empty input shows the error inside the same dialog, and typing drops it; the optional offer
// declines instead. A letter, ? and : are text, and esc closes the dialog.
func TestInputDialogsAnswerEnterEscAndTyping(t *testing.T) {
	for _, c := range append(inputDialogCases(), pathDialogCases()...) {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t)
			m.render()
			asked, _ := m.currentAsk()
			// Unmasked dialogs start with a text; it is cleared first.
			press(t, m, "ctrl+u", "ctrl+k")
			pump(t, m, "enter")
			if c.empty == "" {
				if m.question != nil || m.vaultOffer != nil || m.screen != screenForm {
					t.Fatalf("enter on the empty offer did not decline it: screen %v", m.screen)
				}
				return
			}
			now, ok := m.currentAsk()
			if !ok || now.title != asked.title && c.empty != tPassphraseMismatch || m.question == nil {
				t.Fatalf("enter on an empty input closed the dialog: screen %v", m.screen)
			}
			want := tr(tNoteError, tr(c.empty))
			if !strings.Contains(squashed(askedText(m)), squashed(want)) {
				t.Fatalf("the dialog does not show %q:\n%s", want, askedText(m))
			}
			typeText(t, m, "q?:")
			if m.fail != "" || strings.Contains(askedText(m), "error:") {
				t.Errorf("typing kept the error %q:\n%s", m.fail, askedText(m))
			}
			if typedInput(m) != "q?:" || m.quitting || helpOpen(m) || cmdlineOpen(m) {
				t.Errorf("letters did not stay text: %q, screen %v", typedInput(m), m.screen)
			}
			dialog := m.question
			pump(t, m, "esc")
			if m.question == dialog || dialog.Value() != "" {
				t.Errorf("esc left the dialog open or its text kept: screen %v", m.screen)
			}
		})
	}
}

// A typed or pasted passphrase is in no drawn frame, no key line, no message and no error text, over the
// screen and in the workspace, also once enter took it; every character of it shows as the kit's mask.
func TestNoInputDialogShowsTheTypedValue(t *testing.T) {
	for _, c := range inputDialogCases() {
		for _, paste := range []bool{false, true} {
			for _, size := range [][2]int{{140, 40}, {minimumWidth, minimumHeight}} {
				m := sizedModel(c.build(t), size[0], size[1])
				if paste {
					m.Update(tea.PasteMsg{Content: inputCanary})
				} else {
					typeText(t, m, inputCanary)
				}
				view := m.render()
				var seen []string
				seen = append(seen, view, screenOf(m), askedText(m), m.status, m.fail, m.busy)
				for _, h := range m.question.Hints() {
					seen = append(seen, tr(h.Label))
				}
				if !strings.Contains(ansi.Strip(view), masked(8)) {
					t.Errorf("%s %v: the typed text is not masked:\n%s", c.name, size, ansi.Strip(view))
				}
				pump(t, m, "enter")
				seen = append(seen, m.render(), screenOf(m), askedText(m), m.status, m.fail, m.busy)
				for _, s := range seen {
					if strings.Contains(s, inputCanary) {
						t.Fatalf("%s %v paste %v shows the typed value:\n%s", c.name, size, paste, s)
					}
				}
			}
		}
	}
}

// A paste lands whole at the cursor as the kit cleans it: line breaks at its end go, line breaks and tabs
// inside it become a space each, and other control characters go.
func TestAPasteIntoAMaskedDialogIsCleanedAsTheKitCleansIt(t *testing.T) {
	for paste, want := range map[string]string{
		inputCanary + "\n":    inputCanary,
		"canary\tpass\r\nx\n": "canary pass x",
		"canary\x07\x1bpass":  "canarypass",
	} {
		m := inputDialogCases()[0].build(t)
		m.Update(tea.PasteMsg{Content: paste})
		if got := typedInput(m); got != want {
			t.Errorf("paste %q = %q, want %q", paste, got, want)
		}
	}
}

// The titles of the dialogs read in both languages.
func TestInputDialogTitlesReadInBothLanguages(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		m, _ := startServiceForm(t)
		sizedModel(m, 140, 40)
		m.askSecret("key")
		want := "Store a secret"
		if uiLang == i18n.German {
			want = "Secret speichern"
		}
		if view := ansi.Strip(m.render()); !strings.Contains(view, want) {
			t.Errorf("the secret dialog lacks its title %q:\n%s", want, view)
		}
	})
}
