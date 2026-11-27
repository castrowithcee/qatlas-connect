package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The provider table, the template choice and the version list are screens of their own (screenProviders,
// screenTemplate, screenVersions) that stand as a kit List over the screen they were opened from. The screen
// says which list is open, and syncChoice keeps the modal in line with it, so a question the list leads to
// takes the screen and the list comes back when the question returns to it.

// choiceModal is the list modal of such a screen, told apart from the value picker by its type.
type choiceModal struct{ listModal }

// newChoice builds a kit List that chooses one of items, titled title, on the entry at selected, with the
// buttons of enter and back labelled by the hint groups enter and back. The entry IDs are indexes, since
// a shown value may hold what an ID may not.
func newChoice(spec kittui.ListSpec, title string, items []kittui.ListItem, selected int, enter, back string) (*kittui.List, error) {
	spec.Keys, spec.Title, spec.CloseOutside = listKeys(""), listTitle, true
	spec.Buttons = []kittui.FormButton{{Action: actEnter, Label: hintLabelID(enter), Primary: true},
		{Action: actBack, Label: hintLabelID(back)}}
	list, err := kittui.NewList(spec, keymap, titled{translator(false), title})
	if err == nil {
		err = list.SetItems(items)
	}
	if err != nil {
		return nil, err
	}
	list.Select(choiceID(selected))
	return list, nil
}

func choiceID(i int) kittui.ItemID { return kittui.ItemID(strconv.Itoa(i)) }

// chosen is the index of the marked entry of a list built by newChoice with n entries.
func chosen(list *kittui.List, n int) (int, bool) {
	id, ok := list.Selected()
	i, err := strconv.Atoi(string(id))
	if !ok || err != nil || i < 0 || i >= n {
		return 0, false
	}
	return i, true
}

// screenChoice is the list the current screen stands as, with the function that runs its answers, or nil.
func (m *Model) screenChoice() (*kittui.List, []action, func(action) tea.Cmd) {
	switch {
	case m.screen == screenProviders && m.providers != nil:
		return m.providers.list, []action{actInfo}, m.providerAnswer
	case m.screen == screenTemplate && m.template != nil:
		return m.template.list, nil, m.templateAnswer
	case m.screen == screenVersions && m.versions != nil:
		return m.versions.list, nil, m.versionAnswer
	}
	return nil, nil, nil
}

// syncChoice opens the list of the current screen where no other modal stands, builds the modal anew when
// the list changed, and closes it once the screen is another one. The list stands at the bottom of the
// modals, since it opens only where none is open.
func (m *Model) syncChoice() {
	list, keys, done := m.screenChoice()
	bottom := m.modal
	if len(m.modalsUnder) > 0 {
		bottom = m.modalsUnder[0].part
	}
	shown, ok := bottom.(*choiceModal)
	if ok && shown.List != list {
		if len(m.modalsUnder) > 0 {
			m.modalsUnder = m.modalsUnder[1:]
		} else {
			m.closeModal()
		}
		ok = false
	}
	if list != nil && !ok && m.modal == nil {
		m.showModal(&choiceModal{listModal{List: list, keys: keys}}, done)
	}
}

// underScreen is the screen drawn under the open modal: a list that is a screen of its own draws the screen
// it was opened from.
func (m *Model) underScreen() screen {
	if list, _, _ := m.screenChoice(); list != nil {
		if s, ok := m.screenBeneath(m.screen); ok {
			return s
		}
	}
	return m.screen
}
