package tui

import (
	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// listKeys are the actions a kit List reads; toggle ticks in a list with tick boxes and is empty otherwise. A
// list that filters as typed takes no search key, but its search line is a target of it.
func listKeys(toggle action) kittui.ListKeys {
	return kittui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome,
		Last: actEnd, Left: actLeft, Right: actRight, Enter: actEnter, Search: actSearch, Back: actBack, Toggle: toggle}
}

// listModal stands a kit List, built with listKeys, as the open modal. The list is no kittui.Modal: it answers
// with the entry it opens, which comes back as enter. Back, once no filter is left to clear, and a click
// outside the frame come back as back; any other key the list leaves comes back as the one of keys it is
// bound to under the list's input focus, and a click on a key of the key line or the footer acts as that key.
// It stands as a pointer, so comparing it with another modal never panics.
type listModal struct {
	*kittui.List
	keys []action
	// after runs after every key and click the list took, so the opener keeps its state in line with it.
	after func()
}

func (l listModal) Update(msg tea.KeyPressMsg) (action, bool) {
	if l.Input() == kittui.InputNavigation {
		// A list without a search line moves with the vim letters too, as every screen without text does.
		msg = vimArrow(msg)
	}
	open, handled := l.List.Update(msg)
	l.took(handled)
	switch {
	case open != "":
		return actEnter, true
	case handled:
		return "", true
	}
	for _, a := range append([]action{actBack}, l.keys...) {
		if keymap.Is(msg, a, l.Input()) {
			return a, false
		}
	}
	return "", false
}

func (l listModal) Click(r kittui.MouseResult) (action, bool) {
	open, handled := l.List.Click(r)
	if !handled && r.Item == "" && r.Action != "" && r.Zone != kittui.ZoneOutside {
		open, handled = l.Act(r.Action)
	}
	l.took(handled)
	switch {
	case open != "":
		return actEnter, true
	case handled:
		return "", true
	case r.Zone == kittui.ZoneOutside:
		return actBack, false
	}
	return r.Action, false
}

func (l listModal) took(handled bool) {
	if handled && l.after != nil {
		l.after()
	}
}

// listTitle is the title ID of a list modal whose title titled resolves.
const listTitle textID = "list.title"

// titled is a Translator that gives listTitle the text the opener made, such as a title that names a row.
type titled struct {
	kittui.Translator
	title string
}

func (t titled) Lookup(id textID, args ...any) string {
	if id == listTitle {
		return t.title
	}
	return t.Translator.Lookup(id, args...)
}

func (t titled) Find(id textID) (string, bool) {
	if id == listTitle {
		return t.title, true
	}
	return t.Translator.Find(id)
}
