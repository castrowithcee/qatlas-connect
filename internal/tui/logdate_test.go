package tui

import (
	"strings"
	"testing"
	"time"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// focusLogDate gives the focus to the date field of the bar, the one after the mode.
func focusLogDate(t *testing.T, m *Model) {
	t.Helper()
	m.logs.setFocus(logFocusRows, false)
	m.logs.setFocus(logFocusBar, false)
	press(t, m, "tab")
}

// openLogDates opens the date form from the bar's date field by its keys.
func openLogDates(t *testing.T, m *Model) *kittui.Form {
	t.Helper()
	focusLogDate(t, m)
	press(t, m, "enter")
	f := logDateForm(m)
	if f == nil {
		t.Fatalf("enter on the date did not open the date form:\n%s", m.render())
	}
	return f
}

// Enter on the day opens the calendar and takes a day there; only the save key applies the day and loads it.
func TestLogDateFormEnterOpensTheCalendarAndTheSaveKeyApplies(t *testing.T) {
	f := activityLog(t)
	today := f.localToday()
	m := f.wideModel(false, today)
	openLogDates(t, m)
	loads := m.logs.loadID
	press(t, m, "enter")
	day, _ := time.ParseInLocation(logDateLayout, today, time.Local)
	if view := m.render(); logDateForm(m) == nil || !strings.Contains(view, day.Format("January 2006")) || m.logs.loadID != loads {
		t.Fatalf("enter on the day did not open the calendar alone:\n%s", view)
	}
	press(t, m, "left", "enter")
	yesterday := day.AddDate(0, 0, -1).Format(logDateLayout)
	if form := logDateForm(m); form == nil || form.Values()[logFieldDay].Time.Format(logDateLayout) != yesterday ||
		m.logs.day != today || m.logs.loadID != loads {
		t.Fatalf("the calendar did not take the day into the field alone: day %s", m.logs.day)
	}
	pump(t, m, "f2")
	if m.modal != nil || m.logs.day != yesterday || m.logs.loadID == loads {
		t.Fatalf("the save key did not apply and load the day: modal %v, day %s", m.modal != nil, m.logs.day)
	}
}

// An empty day and a range from after to stay in the form with their problem, and nothing is loaded.
func TestLogDateFormNeverLoadsAWrongPeriod(t *testing.T) {
	f := activityLog(t)
	today := f.localToday()
	m := f.wideModel(false, today)
	m.logs.mode, m.logs.from, m.logs.to = logModeRange, today, today
	openLogDates(t, m)
	loads := m.logs.loadID
	clearField(t, m)
	press(t, m, "f2")
	if form := logDateForm(m); form == nil || form.Focused() != logFieldFrom || !strings.Contains(m.render(), "not a date") {
		t.Fatalf("an empty from was taken:\n%s", m.render())
	}
	typeText(t, m, "2099-01-01")
	press(t, m, "f2")
	if form := logDateForm(m); form == nil || form.Focused() != logFieldTo || !strings.Contains(m.render(), tr(tLogDateOrder)) {
		t.Fatalf("a range ending before it starts was taken:\n%s", m.render())
	}
	if m.logs.loadID != loads || m.logs.from != today || m.logs.to != today {
		t.Fatalf("a refused period was loaded: from %s to %s", m.logs.from, m.logs.to)
	}
}

// Esc, the back button and a click outside close the form without a question and leave the period; the
// screen behind the form takes no click.
func TestLogDateFormClosesOnBackAndAClickOutside(t *testing.T) {
	f := activityLog(t)
	today := f.localToday()
	m := f.wideModel(false, today)
	loads := m.logs.loadID
	openLogDates(t, m)
	typeText(t, m, "x")
	press(t, m, "esc")
	if m.modal != nil || m.screen != screenLogs || m.logs.day != today {
		t.Fatalf("esc did not close the form unchanged: screen %v day %s", m.screen, m.logs.day)
	}
	openLogDates(t, m)
	clickOn(t, m, "esc back")
	if m.modal != nil {
		t.Fatal("the back button did not close the form")
	}
	openLogDates(t, m)
	m.render()
	// The mode field of the bar lies outside the frame: the click closes the form and opens nothing.
	clickOn(t, m, "Mode:")
	if m.modal != nil || m.logs.bar.Open() || m.logs.mode != logModeDay || m.logs.day != today || m.logs.loadID != loads {
		t.Fatalf("a click outside did not only close the form: modal %v values %v", m.modal != nil, m.logs.bar.Open())
	}
}

// The form stands over the Logs where it fits, else in the workspace with its keys in the footer.
func TestLogDateFormAtEverySize(t *testing.T) {
	f := activityLog(t)
	for _, size := range []struct {
		w, h   int
		framed bool
	}{{40, 15, false}, {59, 19, true}, {80, 24, true}, {150, 42, true}} {
		m := sizedModel(f.model(false, f.localToday()), size.w, size.h)
		m.logs.mode = logModeRange
		m.openLogDateDialog()
		view := m.render()
		if framed := strings.Contains(view, "═ Choose a range of local days"); framed != size.framed || m.tooSmall ||
			!strings.Contains(view, "apply") {
			t.Errorf("%dx%d: framed %v, want %v:\n%s", size.w, size.h, framed, size.framed, view)
		}
	}
}
