package tui

import (
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// blockedField is the row that lists the tools a person has blocked on the connection. Its choices are always
// its entries, all ticked, so marked() is the list that is saved; the picker works on the tools instead.
func (m *Model) blockedField(entries []string) field {
	f := field{label: blockedLabel, kind: fieldToolList, selected: map[string]bool{}}
	m.setBlocked(&f, entries)
	f.hint, f.detail = tr(tFormBlockedHint), tr(tFormBlockedDetail)
	return f
}

// setBlocked puts entries on the blocked row.
func (m *Model) setBlocked(f *field, entries []string) {
	f.choices, f.selected = append([]string(nil), entries...), map[string]bool{}
	for _, entry := range entries {
		f.selected[entry] = true
	}
}

// formSelection is how the form selects tools now: its ticked tools in mode selected tools, else nil tools
// and the effects it holds (nil for the provider's defaults).
func (m *Model) formSelection() (tools []string, effects []config.Effect) {
	if list := m.field(toolListLabel); list != nil && m.fieldValue(accessLabel) == accessTools {
		return list.marked(), nil
	}
	effects, _ = config.ParseEffects(m.fieldValue(effectsLabel))
	return nil, effects
}

// blockCandidates are the tools the picker of the blocked row lists: the registered tools the form's
// selection does not offer, then the exact IDs already blocked that the provider no longer registers.
func (m *Model) blockCandidates() []string {
	metadata, _ := m.cfg.ProviderMetadata(m.formProvider())
	tools, effects := m.formSelection()
	known := map[string]bool{}
	var out []string
	for _, tool := range metadata.Tools {
		known[tool.ID] = true
		if !config.SelectionOffers(metadata, tools, effects, tool) {
			out = append(out, tool.ID)
		}
	}
	if row := m.field(blockedLabel); row != nil {
		for _, entry := range row.marked() {
			if !config.IsToolPrefix(entry) && !known[entry] {
				out = append(out, entry)
			}
		}
	}
	return out
}

// blockedTicks ticks the shown tools that an entry of the blocked row covers.
func (m *Model) blockedTicks(shown []string) map[string]bool {
	marks := map[string]bool{}
	row := m.field(blockedLabel)
	if row == nil {
		return marks
	}
	for _, id := range shown {
		for _, entry := range row.marked() {
			if config.BlockedToolCovers(m.formProvider(), entry, id) {
				marks[id] = true
			}
		}
	}
	return marks
}

// blockedFromPicker is the blocked list the ticks of the open picker stand for.
func (m *Model) blockedFromPicker() []string {
	metadata, _ := m.cfg.ProviderMetadata(m.formProvider())
	var previous []string
	if row := m.field(blockedLabel); row != nil {
		previous = row.marked()
	}
	return config.BlockedEntriesFor(metadata, previous, m.pickerShown, m.pickerMarks)
}

// tickedNow are the entries of the tool list as the open picker has them ticked.
func (m *Model) tickedNow() []string {
	if m.blockTicks == nil {
		m.blockTicks = []string{}
		for choice, marked := range m.pickerMarks {
			if marked {
				m.blockTicks = append(m.blockTicks, choice)
			}
		}
	}
	return m.blockTicks
}

// blockNote is the state a tool of the open tool picker is in beyond ticked (offered) and not ticked (not
// offered): blocked, or blocked until the tick is saved. It stays text, so it reads without colour.
func (m *Model) blockNote(index *toolIndex, id string) string {
	row := m.field(blockedLabel)
	if row == nil || m.pickerMarks == nil || m.fields[m.focus].label != toolListLabel {
		return ""
	}
	metadata := index.metadata
	ticks := m.tickedNow()
	offered := m.pickerMarks[id]
	if i, ok := index.byID[id]; ok {
		offered = config.ToolListCovers(metadata.ID, ticks, metadata.Tools[i])
	}
	if m.blockReleased == nil {
		m.blockReleased = map[string]bool{}
	}
	for _, entry := range row.marked() {
		if !config.BlockedToolCovers(metadata.ID, entry, id) {
			continue
		}
		released, ok := m.blockReleased[entry]
		if !ok {
			released = config.BlockedEntryReleased(metadata, entry, ticks)
			m.blockReleased[entry] = released
		}
		switch {
		case offered && released:
			return "  " + tr(tFormBlockEnds)
		case !offered && !released:
			return "  " + tr(tFormToolBlocked)
		}
	}
	return ""
}

// blockedSummary is the value of the blocked row: the entries that still block a tool the form does not
// offer, with how many tools they cover. Entries a tools list will release, or that cover only offered
// tools, are left out, since they block nothing.
func (m *Model) blockedSummary(f field) string {
	metadata, _ := m.cfg.ProviderMetadata(m.formProvider())
	tools, _ := m.formSelection()
	shown := m.blockCandidates()
	var entries []string
	covered := map[string]bool{}
	for _, entry := range config.KeptBlocks(metadata, f.marked(), tools) {
		named := false
		for _, id := range shown {
			if config.BlockedToolCovers(metadata.ID, entry, id) {
				covered[id], named = true, true
			}
		}
		if named {
			entries = append(entries, m.toolEntryLabel(metadata.ID, entry))
		}
	}
	if len(entries) == 0 {
		return emptyValue(wordNone)
	}
	return tr(tFormBlockedSummary, len(covered), strings.Join(entries, ", "))
}
