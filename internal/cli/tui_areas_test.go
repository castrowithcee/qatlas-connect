package cli

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/helptopics"
	"github.com/castrowithcee/qatlas-connect/internal/tui"
)

// areaNumberErrors finds every reference to an area of the editor by its number in text, such as
// "3 Connections", "Activity (4)", "(Access, 5)", "Access > Approvals (5)", or "Approvals (5, second part
// of Access)", and returns how many it checked and each one whose number is not the area's position in
// areas plus one.
func areaNumberErrors(text string, areas []string) (int, []string) {
	names := strings.Join(areas, "|")
	const part = `[A-Z][A-Za-z-]*`
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\b(?P<n>\d+)\s+(?P<area>` + names + `)\b`),
		regexp.MustCompile(`\b(?P<area>` + names + `)\s+\((?P<n>\d+)[,)]`),
		regexp.MustCompile(`\((?P<area>` + names + `),\s+(?P<n>\d+)\)`),
		regexp.MustCompile(`\b(?P<area>` + names + `)\s+>\s+` + part + `\s+\((?P<n>\d+)[,)]`),
		regexp.MustCompile(`\b` + part + `\s+\((?P<n>\d+),[^)]*\bof\s+(?P<area>` + names + `)\)`),
	}
	checked := 0
	var errs []string
	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			area, n := m[re.SubexpIndex("area")], m[re.SubexpIndex("n")]
			got, _ := strconv.Atoi(n)
			checked++
			for i, name := range areas {
				if name == area && got != i+1 {
					errs = append(errs, fmt.Sprintf("%q numbers %s as %d, the tab line as %d", m[0], area, got, i+1))
				}
			}
		}
	}
	return checked, errs
}

// The help of 'qconnect tui' and the help topics number the areas as the tab line does, so the digit a
// text names is the key that opens the area.
func TestHelpNumbersTheAreasAsTheTabLine(t *testing.T) {
	areas := tui.AreaNames()
	texts := map[string]string{"qconnect tui --help": newTUICommand(&Options{}, nil, "").Long}
	for _, topic := range helptopics.All() {
		texts["help topic "+topic.Name] = topic.Text
	}
	total := 0
	for name, text := range texts {
		checked, errs := areaNumberErrors(text, areas)
		total += checked
		for _, err := range errs {
			t.Errorf("%s: %s", name, err)
		}
	}
	// The help of 'qconnect tui' alone numbers every area in its list of them.
	if total < len(areas) {
		t.Fatalf("found %d numbered area references, want at least %d: the patterns no longer match the help", total, len(areas))
	}
}

// A text with a wrong number in each form is caught, so a passing check above means the numbers are right.
func TestAreaNumberErrorsCatchesAWrongNumber(t *testing.T) {
	areas := tui.AreaNames()
	for _, text := range []string{
		"the tab line shows 2 Connections",
		"o opens Activity (3), the log",
		"an Approvals part (Access, 4) lists",
		"approved in Access > Approvals (4).",
		"Tokens (4, first part of Access) lists",
	} {
		checked, errs := areaNumberErrors(text, areas)
		if checked != 1 || len(errs) != 1 {
			t.Errorf("%q: checked %d, errors %q, want one reference with one error", text, checked, errs)
		}
	}
	right := "2 Library, 3 Connections, Activity (4), (Access, 5), Access > Approvals (5), Tokens (5, first part of Access)"
	if checked, errs := areaNumberErrors(right, areas); checked != 6 || len(errs) != 0 {
		t.Errorf("right numbers: checked %d, errors %q, want six references without error", checked, errs)
	}
}
