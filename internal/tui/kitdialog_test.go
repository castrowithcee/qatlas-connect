package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// askedText is the text of the dialog of the question m asks, as it draws it in a workspace, and "" for none.
func askedText(m *Model) string {
	m.syncQuestion()
	if m.question == nil {
		return ""
	}
	view, _ := m.question.View(styles.Styles, 100, 40)
	return ansi.Strip(view)
}

// typedInput is the text of the input of the open question, "" where none is open.
func typedInput(m *Model) string {
	if m.question == nil {
		return ""
	}
	return m.question.Value()
}

// masked is how an input dialog draws n typed characters of a masked input.
func masked(n int) string { return strings.Repeat(styles.Styles.Glyph(kittui.GlyphMask), n) }

// dialogQuestions are the questions of clickDialogs over the screen, without the size of a terminal, and the
// leave questions of the export and the import.
func dialogQuestions() []struct {
	name  string
	build func(t *testing.T) *Model
} {
	var out []struct {
		name  string
		build func(t *testing.T) *Model
	}
	for _, d := range clickDialogs() {
		if !strings.HasSuffix(d.name, " in the workspace") {
			out = append(out, d)
		}
	}
	transfer := func(set func(m *Model)) func(t *testing.T) *Model {
		return func(t *testing.T) *Model {
			m, _, _ := newModel(t)
			set(m)
			m.screen, m.leaveFrom = screenLeave, screenTransfer
			return sizedModel(m, 140, 40)
		}
	}
	return append(out, []struct {
		name  string
		build func(t *testing.T) *Model
	}{
		{"export leave question", transfer(func(m *Model) { m.export = &exportState{step: exportPreview, from: screenList} })},
		{"export plain leave question", transfer(func(m *Model) { m.export = &exportState{step: exportPlainConfirm, from: screenList} })},
		{"import leave question", transfer(func(m *Model) { m.imp = &importState{step: importPreview, from: screenList} })},
	}...)
}

// keepAndDanger are the answer enter gives and the one that destroys of each question of dialogQuestions.
var keepAndDanger = map[string][2]action{
	"delete question": {actNo, actYes}, "remove secret question": {actNo, actYes}, "lock question": {actNo, ""},
	"encryption question": {actNo, ""}, "migrate question": {actNo, ""}, "migrate delete question": {actNo, actYes},
	"approve all question": {actNo, ""}, "revoke question": {actNo, actYes}, "profile question": {actNo, ""},
	"leave question": {actBack, actDiscard}, "quit question": {actBack, actDiscard}, "update question": {actNo, ""},
	"version question": {actNo, ""}, "restore question": {actNo, ""}, "restart question": {actNo, actYes},
	"rename question": {actNo, ""}, "path remove question": {actNo, actYes}, "target remove question": {actNo, actYes},
	"export leave question":       {actBack, actDiscard},
	"export plain leave question": {actBack, actDiscard}, "import leave question": {actBack, actDiscard},
}

// Enter gives the answer that keeps everything, as esc does; the answer that destroys is never it.
func TestEnterGivesTheKeepingAnswerOfEveryQuestion(t *testing.T) {
	for _, q := range dialogQuestions() {
		t.Run(q.name, func(t *testing.T) {
			m := q.build(t)
			asked, ok := m.currentAsk()
			if !ok {
				t.Fatal("the screen asks no question")
			}
			want, listed := keepAndDanger[q.name]
			if !listed || asked.keep != want[0] || asked.danger != want[1] || asked.danger == asked.keep {
				t.Errorf("keep %q, danger %q, want %q", asked.keep, asked.danger, want)
			}
			states := map[string]string{}
			for _, key := range []string{"enter", "esc", firstKey(t, asked.keep)} {
				m := q.build(t)
				m.render()
				pump(t, m, key)
				states[key] = clickState(m)
				// The version question returns to its list, which stands as a modal of its own.
				if m.question != nil || m.modal != nil && !choiceOpen(m, m.screen) {
					t.Errorf("%s left the question open: screen %v", key, m.screen)
				}
			}
			if keep := states[firstKey(t, asked.keep)]; states["enter"] != keep || states["esc"] != keep {
				t.Errorf("enter, esc and %s answer apart:\nenter:\n%s\nesc:\n%s\nkeep:\n%s", asked.keep, states["enter"], states["esc"], keep)
			}
		})
	}
}

// Every question stands over the screen where 36 cells of text fit beside its frame, and fills the workspace
// below that; it fits the terminal at every size.
func TestEveryQuestionFitsAsPopupOrInTheWorkspace(t *testing.T) {
	for _, q := range dialogQuestions() {
		for _, size := range [][2]int{{40, 15}, {59, 19}, {60, 20}, {80, 24}, {150, 42}} {
			m := sizedModel(q.build(t), size[0], size[1])
			view := m.render()
			assertViewFits(t, view, size[0], size[1])
			if m.question == nil || m.modal != m.question {
				t.Errorf("%s %dx%d: the question is no dialog", q.name, size[0], size[1])
				continue
			}
			if popup := strings.Contains(roundFrame(ansi.Strip(view)), "│ "+warnMark+" "); popup != (size[0]-8 >= 36) {
				t.Errorf("%s %dx%d: popup %v:\n%s", q.name, size[0], size[1], popup, ansi.Strip(view))
			}
		}
	}
}

// The help and the command line open over a question by their keys; closed again, they show the question.
func TestHelpAndCommandLineOpenOverAQuestion(t *testing.T) {
	m := sizedModel(groupModel(t), 140, 40)
	openSectionByName(t, m, sectionCredentials)
	entries := len(m.cfg.Credentials)
	press(t, m, keyLabel(actDelete))
	m.render()
	press(t, m, keyLabel(actHelp))
	if !helpOpen(m) {
		t.Fatal("? does not open the help over the question")
	}
	press(t, m, "esc")
	m.render()
	if m.screen != screenConfirm || m.question == nil || m.modal != m.question {
		t.Fatalf("closing the help left screen %v without the question", m.screen)
	}
	press(t, m, keyLabel(actCommand))
	if !cmdlineOpen(m) || m.screen != screenConfirm {
		t.Fatalf(": opens screen %v, want the command line over the question", m.screen)
	}
	press(t, m, "esc")
	m.render()
	if m.screen != screenConfirm || m.question == nil || m.modal != m.question {
		t.Fatalf("closing the command line left screen %v without the question", m.screen)
	}
	if len(m.cfg.Credentials) != entries {
		t.Fatal("the help or the command line answered the question")
	}
}

// A text of a question that carries a secret the redactor knows shows it masked.
func TestNoSecretReachesTheTextOfAQuestion(t *testing.T) {
	const canary = "canary-question-registered"
	m, _ := startServiceForm(t)
	m.redactor.Add(canary)
	m.screen, m.migrateDeleteConfirm, m.migrateResult = screenConfirm, true, "2 entries moved; warning: "+canary
	sizedModel(m, 120, 40)
	if view := ansi.Strip(m.render()); strings.Contains(view, canary) || !strings.Contains(view, "2 entries moved") {
		t.Errorf("the question shows the secret or lacks its text:\n%s", view)
	}
	if text := askedText(m); strings.Contains(text, canary) {
		t.Errorf("the dialog holds the secret:\n%s", text)
	}
}

// ctrl+c in the leave question turns it into the question before quitting and answers nothing: the form
// keeps its input, and esc still returns to it.
func TestCtrlCInTheLeaveQuestionTurnsItIntoQuitting(t *testing.T) {
	m, _ := startServiceForm(t)
	sizedModel(m, 120, 40)
	name := m.fieldValue("name")
	press(t, m, "esc")
	m.render()
	press(t, m, "ctrl+c")
	view := ansi.Strip(m.render())
	if m.quitting || m.screen != screenLeave || !m.leaveQuit || m.question == nil || m.modal != m.question ||
		!strings.Contains(view, "discard and quit") {
		t.Fatalf("ctrl+c answered the question: quitting %v screen %v quit %v:\n%s", m.quitting, m.screen, m.leaveQuit, view)
	}
	press(t, m, "esc")
	if m.screen != screenForm || m.fieldValue("name") != name || m.quitting {
		t.Errorf("esc after ctrl+c: screen %v name %q quitting %v", m.screen, m.fieldValue("name"), m.quitting)
	}
}

// The destroying answer of a question, by key or by its button, still needs the admin session: without one
// it opens the admin dialog and changes nothing.
func TestAnswersStillNeedTheAdminSession(t *testing.T) {
	for _, tc := range []struct {
		name, button string
		ask          func(t *testing.T, m *Model)
	}{
		{"delete", "remove ]", func(t *testing.T, m *Model) { press(t, m, "d") }},
		{"rename", "rename ]", func(t *testing.T, m *Model) {
			pump(t, m, "r")
			clearField(t, m)
			typeText(t, m, "private")
			pump(t, m, "f2")
			unlockWithAdminPassphrase(t, m, tokenPassphrase)
		}},
	} {
		for _, click := range []bool{false, true} {
			store, dir, _ := newTokenFixture(t)
			m := openApprovalModel(t, store, dir)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			before := storedConfig(t, filepath.Join(dir, "config.yaml")).Connections
			openSectionByName(t, m, sectionConnections)
			tc.ask(t, m)
			if m.question == nil {
				t.Fatalf("%s: no question, screen %v", tc.name, m.screen)
			}
			m.endAdminSession()
			m.render()
			if click {
				clickOn(t, m, tc.button)
			} else {
				pump(t, m, "y")
			}
			if m.screen != screenAdminAuth {
				t.Errorf("%s (click %v): screen %v, want the admin dialog", tc.name, click, m.screen)
			}
			if after := storedConfig(t, filepath.Join(dir, "config.yaml")).Connections; len(after) != len(before) || after["personal"].Service == "" {
				t.Errorf("%s (click %v): the connection changed without the admin session", tc.name, click)
			}
		}
	}
}
