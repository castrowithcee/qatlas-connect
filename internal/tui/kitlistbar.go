package tui

import (
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// The filter bar of the grouped lists is the kit's FilterBar over the list of the section, with the search
// field of the list at its right end. listOptions stays the record of its values: the bar is made from it and
// every change it reports is written back there, so the list reads the same values (see listbar.go).

// barIDs name the fields of the bar for the kit; the values are named by their English words.
var barIDs = map[barKind]kit.FieldID{barGroup: "group", barState: "state", barSort: "sort"}

// filterBarKeys are the keys the filter bars read.
var filterBarKeys = kit.FilterBarKeys{Left: actLeft, Right: actRight, Next: actNext, Prev: actPrev, Up: actUp,
	Down: actDown, Enter: actEnter, Back: actBack, PageUp: actPageUp, PageDown: actPageDown}

// bar is the filter bar of the active section, made on first use with the values of its listOptions, and
// anew with its list in another language.
func (m *Model) bar() *kit.FilterBar {
	list := m.listFor(m.section)
	if b := m.bars[m.section]; b != nil {
		return b
	}
	var fields []kit.FilterField
	for _, k := range barKinds(m.section) {
		label, values, active := m.barField(k)
		f := kit.FilterField{ID: barIDs[k], Label: catalogID(wordID(label), label)}
		for _, v := range values {
			f.Options = append(f.Options, kit.Choice{ID: kit.ChoiceID(v), Label: catalogID(wordID(v), v)})
		}
		f.Selected = f.Options[active].ID
		fields = append(fields, f)
	}
	b, err := kit.NewFilterBar(kit.FilterBarSpec{Fields: fields, Keys: filterBarKeys, List: list}, keymap, translator(false))
	if err != nil {
		panic("the filter bar of a section is invalid: " + err.Error())
	}
	if m.bars == nil {
		m.bars = map[section]*kit.FilterBar{}
	}
	m.bars[m.section] = b
	return b
}

// barFocused reports whether a field of the filter bar of the shown list has the focus.
func (m *Model) barFocused() bool {
	b := m.bars[m.section]
	return b != nil && b.Focused() && m.screen == screenList && m.listBarShown()
}

// barOpen reports whether the list of values of a field or the filter modal is open; either takes every key.
func (m *Model) barOpen() bool { return m.barFocused() && m.bars[m.section].Open() }

// barValuesOpen reports whether the list of values of a field is open, whose search line reads text.
func (m *Model) barValuesOpen() bool { return m.barOpen() && len(m.bars[m.section].Hints()) == 0 }

// focusBar gives the focus to the first field of the bar, unless a field has it already.
func (m *Model) focusBar() {
	m.leaveDetail()
	m.noticeFocus = false
	if b := m.bar(); !b.Focused() {
		b.Focus(false)
	}
}

// blurBar takes the focus from every filter bar, closing an open list of values or modal without change.
func (m *Model) blurBar() {
	for _, b := range m.bars {
		b.Blur()
	}
}

// applyBar writes the fields the bar changed into the listOptions of the section and lists anew.
func (m *Model) applyBar(changed []kit.FilterField) {
	for _, f := range changed {
		for _, k := range barKinds(m.section) {
			if barIDs[k] == f.ID {
				_, values, _ := m.barField(k)
				m.setBar(k, slices.Index(values, string(f.Selected)))
			}
		}
	}
}

// barView draws the bar in width cells. The bar plans whether its fields give way to the button as it draws,
// so the keys read the plan of the width the bar stands in.
func (m *Model) barView(width int) (string, []kit.Target) { return m.bar().View(styles.Styles, width) }

// barCompact reports whether the fields of the bar gave way to the filter button: the bar names one target
// per drawn field or the button.
func (m *Model) barCompact() bool {
	_, targets := m.barView(m.tableRoom())
	items := 0
	for _, t := range targets {
		if t.Item != "" {
			items++
		}
	}
	return items < len(barKinds(m.section))
}

// updateBar is a key on the focused filter bar while neither its values nor its modal are open. The bar
// steps through its fields and changes their values; at its ends tab goes on to the tab line and shift+tab
// back to the pane or the rows, up and back go to the tab line, and down to the rows. A key of the rows or of
// the screen leaves the bar for the rows, which take it.
func (m *Model) updateBar(key tea.KeyPressMsg) (tea.Cmd, bool) {
	m.barView(m.tableRoom())
	changed, handled := m.bar().Update(key)
	m.applyBar(changed)
	switch {
	case handled:
	case keyIs(key, actNext) || keyIs(key, actUp) || keyIs(key, actBack):
		m.focusNav()
	case keyIs(key, actPrev):
		m.blurBar()
		if m.paneWidth() > 0 {
			m.detailFocus, m.detailTab = true, tabOverview
		}
	case keyIs(key, actDown):
		m.blurBar()
	case keyIs(key, actPageUp) || keyIs(key, actPageDown) || keyIs(key, actHome) || keyIs(key, actEnd) || keyIs(key, actSearch) ||
		keyIs(key, actSetup) || keyIs(key, actUpdate) || keyIs(key, actQuit) || keyIs(key, actNew):
		m.blurBar()
		return nil, false
	default:
		if _, ok := sectionShortcut(key.String()); ok {
			m.blurBar()
			return nil, false
		}
	}
	return nil, true
}

// updateOpenBar is a key while the values of a field or the filter modal are open: they take every key.
func (m *Model) updateOpenBar(key tea.KeyPressMsg) tea.Cmd {
	if content, ok := pasted(key); ok {
		// The bar has no paste of its own: the pasted text is typed into the search of the values, the
		// control characters left out.
		for _, r := range content {
			if !unicode.IsControl(r) {
				m.bar().Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
		return nil
	}
	changed, _ := m.bar().Update(key)
	m.applyBar(changed)
	return nil
}

// clickBar is a click on a target of the filter bar, its values or its modal, as its key does: the free area
// focuses the first field as f does, a field focuses and opens its values, a value takes it, the button opens
// the modal, and the search field starts the search of the list, which then has the focus. A key of the key
// line of the modal is pressed.
func (m *Model) clickBar(r kit.MouseResult) tea.Cmd {
	if m.screen == screenNav {
		if cmd := m.focusList(); m.screen != screenList {
			return cmd
		}
	}
	if m.screen != screenList || !m.listBarShown() {
		return nil
	}
	changed, handled := m.bar().Click(r)
	m.applyBar(changed)
	switch {
	case handled:
		m.leaveDetail()
		m.noticeFocus = false
	case r.Item == "" && r.Action == actSearch:
		m.blurBar()
		m.leaveDetail()
		m.noticeFocus = false
		m.list.Click(r)
	case r.Item == "" && r.Action == "":
		m.focusBar()
	case r.Action != "":
		return m.pressAction(r.Action)
	}
	return nil
}

// barKeys is the key line while the filter bar has the focus: the keys of the open modal, of the open list of
// values, or of the fields, where the button takes their place without left and right.
func (m *Model) barKeys() string {
	b := m.bar()
	if hints := b.Hints(); len(hints) > 0 {
		return hintsText(hints)
	}
	if b.Open() {
		return "type to filter · {up}/{down} move · {enter} choose · {back} cancel"
	}
	keys := "{left}/{right} change · {enter} open · "
	if m.barCompact() {
		keys = "{enter} open · "
	}
	return keys + "{next} next · {prev} previous · {down} rows · {search} filter · {new} new · {area}/{back}/{up} areas · " +
		"{command} cmds · {help} help · {quit} quit"
}

// hintsText is the kit's hints as a key text, each behind its key with the label in the language in force.
func hintsText(hints []kit.Hint) string {
	groups := make([]string, len(hints))
	for i, h := range hints {
		groups[i] = footerPlaceholder(h.Action) + " " + tr(h.Label)
	}
	return strings.Join(groups, " · ")
}

// addBarTargets places the targets the bar, its values or its modal drew from column x and row y on the screen.
// A filter being typed has none, nor has a screen drawn behind a dialog.
func (m *Model) addBarTargets(targets []kit.Target, x, y int) {
	if m.backdrop || m.tooSmall || m.list.Searching() {
		return
	}
	m.barTargets = append(m.barTargets, kit.OffsetTargets(targets, x, y)...)
}

// barModalOverlay draws the open filter modal over screen, the whole terminal drawn behind it; ok is false
// while it is closed and where the terminal has no room for it, which then shows it in the workspace.
func (m *Model) barModalOverlay(screen string) (string, []kit.Target, bool) {
	b := m.bars[m.section]
	if !m.barFocused() || len(b.Hints()) == 0 {
		return "", nil, false
	}
	return b.ModalOverlay(styles.Styles, screen, m.termWidth, m.termHeight, kit.Rect{Width: m.termWidth, Height: m.termHeight})
}

// barModalView is the frame with the open filter modal over it, where it fits. The screen behind it is drawn
// as the background of a dialog, and the modal's targets are the only ones.
func (m *Model) barModalView() (string, bool) {
	if _, _, ok := m.barModalOverlay(""); !ok {
		return "", false
	}
	m.backdrop = true
	screen := m.frame(m.workspaceView())
	m.backdrop = false
	view, targets, ok := m.barModalOverlay(screen)
	if !ok || m.tooSmall {
		return screen, true
	}
	m.hits, m.zones, m.listTargets, m.frameTabs, m.frameKeys = nil, nil, nil, nil, nil
	m.barTargets = targets
	// The modal covers the screen: a click beside it reaches nothing.
	m.dialogBox = hit{x1: m.termWidth, y1: m.termHeight}
	return view, true
}
