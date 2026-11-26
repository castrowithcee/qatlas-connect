package manage

import (
	"slices"
	"sort"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// removePlan is the change accepting a request to remove tools makes, computed once for the preview and
// the save.
type removePlan struct {
	// cfg is the configuration after accepting. For a connection by effect it is the switch to a tools list.
	cfg *config.Config
	// removed are the IDs of the tools the connection stops offering.
	removed []string
	// remaining is the number of tools the connection offers afterwards.
	remaining int
	// split are the prefix entries of the tools list that were replaced by the exact IDs of their other tools.
	split []string
}

// planRemove computes the configuration without the selected tools. A connection with a tools list drops
// the exact entries of the selection; a prefix entry that covers a selected tool is replaced by the exact
// IDs of the tools it still covers and the connection offers today. A connection by effect becomes a tools
// list of every tool it offers except the selected ones.
func (d *decisionState) planRemove() removePlan {
	var plan removePlan
	removed := map[string]bool{}
	for _, entry := range d.selected {
		for _, tool := range d.covered(entry) {
			if d.offers(d.cfg, tool) {
				removed[tool.ID] = true
			}
		}
	}
	for id := range removed {
		plan.removed = append(plan.removed, id)
	}
	sort.Strings(plan.removed)

	own := d.ownTools()
	keepsToday := func(tool config.ToolMetadata) bool { return d.offers(d.cfg, tool) && !removed[tool.ID] }
	if list := d.cfg.Connections[d.name].Tools; list != nil {
		plan.cfg = d.withConnection(func(c *config.Connection) {
			next := []string{}
			for _, entry := range list {
				if !config.IsToolPrefix(entry) {
					if !removed[entry] {
						next = append(next, entry)
					}
					continue
				}
				hit := false
				var keep []string
				for _, tool := range own {
					if !config.ToolPrefixCovers(d.provider, entry, tool) {
						continue
					}
					if removed[tool.ID] {
						hit = true
					} else if keepsToday(tool) {
						keep = append(keep, tool.ID)
					}
				}
				if !hit {
					next = append(next, entry)
					continue
				}
				plan.split = append(plan.split, entry)
				for _, id := range keep {
					if !slices.Contains(next, id) {
						next = append(next, id)
					}
				}
			}
			c.Tools = next
		})
	} else {
		plan.cfg = d.withConnection(func(c *config.Connection) {
			c.Effects = nil
			c.Tools = []string{}
			for _, tool := range own {
				if keepsToday(tool) {
					c.Tools = append(c.Tools, tool.ID)
				}
			}
			sort.Strings(c.Tools)
		})
	}
	for _, tool := range own {
		if d.offers(plan.cfg, tool) {
			plan.remaining++
		}
	}
	return plan
}

// removalHolds is the check before saving: the connection offers none of the removed tools any more and
// still offers every other tool it offered.
func (d *decisionState) removalHolds(plan removePlan) bool {
	for _, tool := range d.ownTools() {
		want := d.offers(d.cfg, tool) && !slices.Contains(plan.removed, tool.ID)
		if d.offers(plan.cfg, tool) != want {
			return false
		}
	}
	return true
}
