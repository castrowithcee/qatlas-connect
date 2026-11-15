package github

import (
	"sort"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// expectedTool is one tool the registry must hold. born marks a tool that was registered with a hyphen from
// the start and so has no former ID; allow marks a tool offered only where a connection's tools list names
// it; output is a value of the Go type the handler returns (a generic map carries a literal with the members
// the handler sets); risk is the contract the tool publishes.
type expectedTool struct {
	id     string
	born   bool
	allow  bool
	output any
	risk   capability.Risk
}

// expectedTools holds the expected tools of every group. Each group registers its own list from its own
// file (toolids_<group>_test.go), so a new tool changes only the lines of its group.
var expectedTools = map[string][]expectedTool{}

func addExpectedTools(group string, tools []expectedTool) {
	if _, dup := expectedTools[group]; dup {
		panic("duplicate expected tool group " + group)
	}
	expectedTools[group] = tools
}

// expectedToolIDs returns the IDs of the tools satisfying keep, sorted.
func expectedToolIDs(keep func(expectedTool) bool) []string {
	var ids []string
	for _, tools := range expectedTools {
		for _, tool := range tools {
			if keep(tool) {
				ids = append(ids, tool.id)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func allExpectedTools(expectedTool) bool { return true }

func readTools(tool expectedTool) bool { return tool.risk.Effect == capability.EffectRead }

// expectedByID indexes every expected tool by its ID.
func expectedByID() map[string]expectedTool {
	byID := map[string]expectedTool{}
	for _, tools := range expectedTools {
		for _, tool := range tools {
			byID[tool.id] = tool
		}
	}
	return byID
}
