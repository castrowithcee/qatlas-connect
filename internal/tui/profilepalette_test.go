package tui

import (
	"strings"
	"testing"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// With 16 colours the heat levels are shade glyphs in the success colour, so four levels stay apart.
func TestHeatWithSixteenColoursUsesShadeGlyphs(t *testing.T) {
	restoreRenderer(t)
	t.Setenv("NO_COLOR", "")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	setProfile(kittui.ProfileANSI16)
	setTheme(darkTheme())
	seen := map[string]bool{}
	for level := 0; level <= 4; level++ {
		mark := heatBlock(level, 2)
		if strings.Contains(mark, "■") {
			t.Errorf("level %d is a square at 16 colours: %q", level, mark)
		}
		seen[mark] = true
	}
	if len(seen) != 5 {
		t.Errorf("the five levels are not told apart: %v", seen)
	}
}

// An unsigned log is unverified on every row; those rows are dimmed while a gap or an altered line keeps its
// warning, and the word stays in the cell.
func TestLogStatusToneDimsOnlyUnverified(t *testing.T) {
	restoreRenderer(t)
	setProfile(kittui.ProfileTrueColor)
	setTheme(darkTheme())
	if got, want := colourOf(logStatusTone(logUnverified.label())), colourOf(styles.hint); got != want {
		t.Errorf("unverified rows are %q, want the dimmed %q", got, want)
	}
	if got, want := colourOf(logStatusTone(logAltered.label())), colourOf(styles.fail); got != want {
		t.Errorf("altered rows are %q, want %q", got, want)
	}
	if got, want := colourOf(logStatusTone(logGap.label())), colourOf(styles.warning); got != want {
		t.Errorf("gap rows are %q, want %q", got, want)
	}
}
