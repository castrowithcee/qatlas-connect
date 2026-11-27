package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTickAndRadioMarksShareOneColour(t *testing.T) {
	on := styles.ok.Render("[x]")
	if got := tickBox(true); got != on {
		t.Errorf("tickBox(true) = %q, want %q", got, on)
	}
	if got := ansi.Strip(tickBox(true)); got != "[x]" {
		t.Errorf("a tick must read as a character: %q", got)
	}
}
