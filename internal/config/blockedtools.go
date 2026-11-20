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
		var kept []string
		for _, entry := range conn.BlockedTools {
			if !blockedEntryCoveredByList(metadata, entry, conn.Tools) {
				kept = append(kept, entry)
			}
		}
		if len(kept) == len(conn.BlockedTools) {
			continue
		}
		conn.BlockedTools = kept
		c.Connections[name] = conn
		changed = append(changed, name)
	}
	return changed
}

// blockedEntryCoveredByList reports whether tools selects every registered tool that the blocked_tools
// entry names, and at least one.
func blockedEntryCoveredByList(metadata ProviderMetadata, entry string, tools []string) bool {
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
