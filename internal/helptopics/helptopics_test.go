package helptopics

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/helptopics"
)

// The topics are plain ASCII, so a rune is a cell wherever they are shown, and each has a unique name.
func TestTopicsArePlainAndDistinct(t *testing.T) {
	names := map[string]bool{}
	for _, topic := range All() {
		if names[topic.Name] || topic.Name == "" || topic.Short == "" || topic.Text == "" {
			t.Errorf("topic %q is unnamed, repeated, or empty", topic.Name)
		}
		names[topic.Name] = true
		for i, r := range topic.Text {
			if r > '~' || (r < ' ' && r != '\n') {
				t.Errorf("topic %s has the character %q at byte %d", topic.Name, r, i)
			}
		}
	}
}

func TestWrapFitsEveryWidthAndKeepsTheText(t *testing.T) {
	for _, topic := range All() {
		for _, width := range []int{1, 2, 7, 20, 36, 40, 56, 72, 80, 200} {
			wrapped := helptopics.Wrap(topic.Text, width)
			for _, line := range strings.Split(wrapped, "\n") {
				if utf8.RuneCountInString(line) > width {
					t.Errorf("%s at %d: line %q is wider", topic.Name, width, line)
				}
			}
			if width >= 36 && strings.Join(strings.Fields(wrapped), " ") != strings.Join(strings.Fields(topic.Text), " ") {
				t.Errorf("%s at %d: wrapping changed the words", topic.Name, width)
			}
		}
		if helptopics.Wrap(topic.Text, 1<<20) != topic.Text {
			t.Errorf("%s: a line that fits was changed", topic.Name)
		}
	}
}

func TestTopicsExplainLocalFiles(t *testing.T) {
	var configuration, agents string
	for _, topic := range All() {
		switch topic.Name {
		case "configuration":
			configuration = topic.Text
		case "agents":
			agents = topic.Text
		}
	}
	for _, want := range []string{"files.read", "files.write", "no-local-files", "needs a new approval"} {
		if !strings.Contains(configuration, want) {
			t.Errorf("the configuration topic does not mention %q", want)
		}
	}
	if !strings.Contains(agents, "no-local-files") {
		t.Error("the agents topic does not name the refusal no-local-files")
	}
}

// The term IDs are a stable contract: the list is fixed here, so a rename or a removal fails this test.
func TestTermIDsAreUniqueAndStable(t *testing.T) {
	want := []string{"term.service", "term.credential", "term.connection", "term.target", "term.effect",
		"term.profile", "term.approval", "term.token", "term.vault"}
	terms := Terms()
	if len(terms) != len(want) {
		t.Fatalf("%d terms, want %d", len(terms), len(want))
	}
	seen := map[string]bool{}
	for i, term := range terms {
		if term.ID != want[i] {
			t.Errorf("term %d has ID %q, want %q", i, term.ID, want[i])
		}
		if seen[term.ID] || term.Title == "" || term.Text == "" {
			t.Errorf("term %q is repeated or empty", term.ID)
		}
		seen[term.ID] = true
		if n := strings.Count(term.Text, ". ") + 1; n > 3 {
			t.Errorf("term %q has %d sentences, want at most 3", term.ID, n)
		}
		for _, r := range term.Text {
			if r > '~' || r < ' ' {
				t.Errorf("term %q has the character %q", term.ID, r)
			}
		}
	}
	if err := helptopics.CheckTerms(terms); err != nil {
		t.Error(err)
	}
}

// An agent with a shell learns where a tool that waits ends: the command, its limit, and its exit codes.
func TestAgentsTopicNamesTheWaitCommand(t *testing.T) {
	for _, want := range []string{"qconnect wait <tool-id>", "45 minutes", "0 pass, 3 fail, 4 timeout, 5 none, 6 head-changed"} {
		if !strings.Contains(Agents().Text, want) {
			t.Errorf("the agents topic does not mention %q", want)
		}
	}
}
