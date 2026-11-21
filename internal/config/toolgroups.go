package config

import (
	"sort"
	"strings"
)

// UnlistedToolGroup is the ID of the last group of a ToolGrouping: the tools the provider no longer
// registers, which a saved list or a stored request may still name.
const UnlistedToolGroup = "not-registered"

// ToolGrouping sorts a list of tool IDs into the groups of one provider. It is presentation only: it never
// changes a tool ID, a permission, or a stored tools list.
type ToolGrouping struct {
	// Groups are the non-empty groups in display order.
	Groups []ToolGroup
	// Members lists the tools of each group by group ID, in the order they were given.
	Members map[string][]string
	// Of maps each tool to its group ID.
	Of map[string]string
}

// GroupTools groups choices by the groups of the provider's metadata, in the order of the provider. A
// provider without groups is grouped by the first ID segment after its own ID, in alphabetical order. A
// tool the provider no longer registers goes into a last group of its own, titled unlistedTitle. It returns
// nil when the provider has no groups, or fewer than two derived ones.
func GroupTools(metadata ProviderMetadata, choices []string, unlistedTitle string) *ToolGrouping {
	known := map[string]string{}
	for _, tool := range metadata.Tools {
		known[tool.ID] = tool.Group
	}
	candidates := append([]ToolGroup{}, metadata.Groups...)
	if len(candidates) == 0 {
		// The provider ID may hold a dot itself, so the prefix is cut, not the second segment taken.
		seen := map[string]bool{}
		for id := range known {
			if rest, ok := strings.CutPrefix(id, metadata.ID+"."); ok {
				if segment, _, more := strings.Cut(rest, "."); more && segment != "" {
					known[id] = segment
					if !seen[segment] {
						seen[segment] = true
						candidates = append(candidates, ToolGroup{ID: segment, Title: segment})
					}
					continue
				}
			}
			delete(known, id)
		}
		if len(candidates) < 2 {
			return nil
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	}
	if len(candidates) == 0 {
		return nil
	}
	g := &ToolGrouping{Members: map[string][]string{}, Of: map[string]string{}}
	for _, choice := range choices {
		id, ok := known[choice]
		if !ok {
			id = UnlistedToolGroup
		}
		g.Members[id] = append(g.Members[id], choice)
		g.Of[choice] = id
	}
	candidates = append(candidates, ToolGroup{ID: UnlistedToolGroup, Title: unlistedTitle})
	for _, group := range candidates {
		if len(g.Members[group.ID]) > 0 {
			g.Groups = append(g.Groups, group)
		}
	}
	return g
}

// GroupOfEntry returns the group a tools entry belongs to: the group of the tool itself, or for a prefix the
// group of the first registered tool below it. ok is false for an entry that names no registered tool or
// for a provider without groups.
func GroupOfEntry(metadata ProviderMetadata, entry string) (ToolGroup, bool) {
	var tool ToolMetadata
	found := false
	if IsToolPrefix(entry) {
		stem, valid := ToolPrefixStem(metadata.ID, entry)
		for _, t := range metadata.Tools {
			if valid && strings.HasPrefix(t.ID, stem) {
				tool, found = t, true
				break
			}
		}
	} else {
		for _, t := range metadata.Tools {
			if t.ID == entry {
				tool, found = t, true
				break
			}
		}
	}
	if !found {
		return ToolGroup{}, false
	}
	g := GroupTools(metadata, []string{tool.ID}, "")
	if g == nil || len(g.Groups) == 0 {
		return ToolGroup{}, false
	}
	return g.Groups[0], true
}
