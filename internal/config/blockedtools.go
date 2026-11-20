package config

import "strings"

// BlockedToolCovers reports whether one blocked_tools entry of provider names toolID, as the exact ID or as
// a valid prefix. Unlike a tools list, a prefix here also covers a tool that requires its exact ID in the
// allow-list: a block only withholds the tool from being requested and releases nothing.
func BlockedToolCovers(provider, entry, toolID string) bool {
	if entry == toolID {
		return true
	}
	stem, ok := ToolPrefixStem(provider, entry)
	return ok && strings.HasPrefix(toolID, stem)
}

// BlocksTool reports whether the connection name blocks the tool toolID from being requested. It looks only
// at the connection's blocked_tools: a block never locks a tool, so the answer says nothing about whether
// the connection offers it.
func (c *Config) BlocksTool(name, toolID string) bool {
	conn, ok := c.Connections[name]
	if !ok {
		return false
	}
	provider := c.Services[conn.Service].Provider
	for _, entry := range conn.BlockedTools {
		if BlockedToolCovers(provider, entry, toolID) {
			return true
		}
	}
	return false
}

// ReleaseCoveredBlocks removes from every connection with a tools list the blocked_tools entries that the
// list now covers: an entry stays only while some registered tool it names is not selected by the list.
// An entry that names no registered tool is kept, since validation reports it. It returns the names of the
// connections it changed, sorted.
func (c *Config) ReleaseCoveredBlocks() []string {
	var changed []string
	for _, name := range sortedKeys(c.Connections) {
		conn := c.Connections[name]
		if conn.Tools == nil || len(conn.BlockedTools) == 0 {
			continue
		}
		metadata, known := c.ProviderMetadata(c.Services[conn.Service].Provider)
		if !known {
			continue
		}
		kept := KeptBlocks(metadata, conn.BlockedTools, conn.Tools)
		if len(kept) == len(conn.BlockedTools) {
			continue
		}
		conn.BlockedTools = kept
		c.Connections[name] = conn
		changed = append(changed, name)
	}
	return changed
}

// KeptBlocks is the blocked_tools list that stays once the tools list is saved: ReleaseCoveredBlocks for one
// list. Without a tools list (nil) nothing is released.
func KeptBlocks(metadata ProviderMetadata, blocked, tools []string) []string {
	if tools == nil {
		return blocked
	}
	var kept []string
	for _, entry := range blocked {
		if !BlockedEntryReleased(metadata, entry, tools) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// BlockedEntryReleased reports whether tools selects every registered tool that the blocked_tools entry
// names, and at least one: the entry then ends when the list is saved.
func BlockedEntryReleased(metadata ProviderMetadata, entry string, tools []string) bool {
	named := 0
	for _, tool := range metadata.Tools {
		if !BlockedToolCovers(metadata.ID, entry, tool.ID) {
			continue
		}
		named++
		if !ToolListCovers(metadata.ID, tools, tool) {
			return false
		}
	}
	return named > 0
}

// SelectionOffers reports whether a connection that selects by tools (non-nil), or else by effects (nil
// effects stand for the provider's defaults), offers tool by that selection alone. Targets, files and the
// connection's state are not judged here.
func SelectionOffers(metadata ProviderMetadata, tools []string, effects []Effect, tool ToolMetadata) bool {
	if tools == nil && effects == nil {
		effects = defaultEffectsOf(metadata)
	}
	// Both directions of local files are released here, so only the selection decides.
	open := Files{Read: []string{"-"}, Write: []string{"-"}}
	return listRefusalOf(metadata.ID, tools, effects, open, tool) == ""
}

// defaultEffectsOf are the effects of a connection that names none.
func defaultEffectsOf(metadata ProviderMetadata) []Effect {
	if len(metadata.DefaultEffects) > 0 {
		return append([]Effect(nil), metadata.DefaultEffects...)
	}
	return []Effect{EffectRead}
}

// BlockedEntriesFor turns the ticks of a picker into a blocked_tools list. shown are the tool IDs the picker
// listed and ticked says which of them are blocked. A previous entry that covers none of the shown tools is
// kept untouched, one whose shown tools are all ticked stays as it is, and any other previous entry ends in
// favour of the IDs still ticked. A stem of at least two registered tools whose tools are all ticked becomes
// one prefix entry, so a group is stored as a prefix; every other tick is its exact ID.
func BlockedEntriesFor(metadata ProviderMetadata, previous, shown []string, ticked map[string]bool) []string {
	var entries []string
	covered := map[string]bool{}
	for _, entry := range previous {
		var named []string
		all := true
		for _, id := range shown {
			if BlockedToolCovers(metadata.ID, entry, id) {
				named = append(named, id)
				all = all && ticked[id]
			}
		}
		if len(named) == 0 || all {
			entries = append(entries, entry)
			for _, id := range named {
				covered[id] = true
			}
		}
	}
	registered := map[string][]string{}
	var stems []string
	for _, tool := range metadata.Tools {
		if stem, ok := idStem(metadata.ID, tool.ID); ok {
			if len(registered[stem]) == 0 {
				stems = append(stems, stem)
			}
			registered[stem] = append(registered[stem], tool.ID)
		}
	}
	for _, stem := range stems {
		ids := registered[stem]
		all := len(ids) >= 2
		for _, id := range ids {
			all = all && ticked[id] && !covered[id]
		}
		if all {
			entries = append(entries, stem+"*")
			for _, id := range ids {
				covered[id] = true
			}
		}
	}
	for _, id := range shown {
		if ticked[id] && !covered[id] {
			entries = append(entries, id)
			covered[id] = true
		}
	}
	return entries
}

// idStem is the segment stem "<provider>.<segment>." of a tool ID that has a segment below the provider.
func idStem(provider, id string) (string, bool) {
	rest, ok := strings.CutPrefix(id, provider+".")
	if !ok {
		return "", false
	}
	segment, _, more := strings.Cut(rest, ".")
	if !more || segment == "" {
		return "", false
	}
	return provider + "." + segment + ".", true
}
